package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// The vendor-object harness both test tiers read, so this file carries no build tag. It holds the
// naming rules, the paged list readers, and the one sweeper that removes what a run left behind.

const (
	testNamePrefix = "pulumi-test-"

	// sweepMinAge keeps the sweeper away from an object that a concurrent run just created.
	sweepMinAge = time.Hour
	// sweepWindow bounds the list walk; a leftover older than this is not from a crashed run.
	sweepWindow = 7 * 24 * time.Hour
	// sweepPageSize is the page size the sweeper asks for; the endpoint defaults to the same 20.
	sweepPageSize = 20
	// sweepMaxPages bounds the walk when a page holds no test object.
	sweepMaxPages = 5
)

// newTestName builds a collision-free vendor object name. crypto/rand.Read fills the buffer or
// panics inside the runtime, so it reports no error worth handling.
func newTestName(kind string) string {
	var raw [4]byte
	_, _ = rand.Read(raw[:])
	return testNamePrefix + kind + "-" + hex.EncodeToString(raw[:])
}

// collectSweepableIDs walks every page before the first delete: a delete shifts the later pages
// and hides an entry behind it. A blank identifier reaches the whole collection, so it is skipped.
func collectSweepableIDs[T any](
	ctx context.Context, key, kind string,
	list func(context.Context, string, int) ([]T, error),
	identify func(T) string,
	sweepable func(T) bool,
) []string {
	var ids []string
	for page := range sweepMaxPages {
		found, err := list(ctx, key, page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "the sweeper could not list the %s objects: %v\n", kind, err)
			return ids
		}
		if len(found) == 0 {
			return ids
		}
		for _, item := range found {
			if id := identify(item); id != "" && sweepable(item) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// sweeper removes the vendor objects of one kind. One sweeper serves every kind, and each kind
// supplies what differs: how to list its objects, how to read a run's marker, and how to delete one.
type sweeper[T any] struct {
	kind    string
	list    func(ctx context.Context, key string, page int) ([]T, error)
	id      func(T) string
	markers func(T) []string
	remove  func(ctx context.Context, client Client, id string) error
	// pattern matches only the markers this package builds, so a caller cannot widen the blast
	// radius by naming a value some other program wrote.
	pattern *regexp.Regexp
	// stale reports whether one object is a leftover of a crashed run: the kind's own predicate,
	// which reads the marker and the age off the list projection.
	stale func(T, time.Time) bool
}

// sweep deletes every object that sweepable accepts, and answers how many it removed. It never
// fails the run: a sweep problem must not hide the result of the tests it prepares for.
func (s sweeper[T]) sweep(ctx context.Context, key string, sweepable func(T) bool) int {
	client, err := NewClient(defaultBaseURL, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "the sweeper could not build a client: %v\n", err)
		return 0
	}

	removed := 0
	for _, id := range collectSweepableIDs(ctx, key, s.kind, s.list, s.id, sweepable) {
		if err := s.remove(ctx, client, id); err != nil && !IsGone(err) {
			fmt.Fprintf(os.Stderr, "the sweeper could not delete a test %s: %v\n", s.kind, err)
			continue
		}
		removed++
	}
	return removed
}

// sweepStale removes what a crashed run left behind, so a rerun starts clean.
func (s sweeper[T]) sweepStale(ctx context.Context, key string) int {
	now := time.Now().UTC()
	return s.sweep(ctx, key, func(item T) bool { return s.stale(item, now) })
}

// markedSweepable answers the predicate that matches one run's objects, or nil when the marker is
// not one this package builds. An object carries exactly one marker, or it is not ours.
func (s sweeper[T]) markedSweepable(marker string) func(T) bool {
	if !s.pattern.MatchString(marker) {
		return nil
	}
	return func(item T) bool {
		got := s.markers(item)
		return len(got) == 1 && got[0] == marker
	}
}

// sweepMarked removes every object of one test run, and nothing else.
func (s sweeper[T]) sweepMarked(ctx context.Context, key, marker string) int {
	sweepable := s.markedSweepable(marker)
	if sweepable == nil {
		return 0
	}
	return s.sweep(ctx, key, sweepable)
}

// pagedQuery is the query every paged list reader starts from: one page of the newest objects.
func pagedQuery(page int) url.Values {
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("size", strconv.Itoa(sweepPageSize))
	query.Set("sort", "DESC")
	return query
}

// sinceTheSweepWindow is the oldest creation time a list reader asks for.
func sinceTheSweepWindow() string {
	return time.Now().UTC().Add(-sweepWindow).Format(time.RFC3339)
}

// listPage reads one page of a collection. The Client interface carries no list method by design,
// so the sweeper builds its own request here rather than widening that surface.
func listPage[T any](ctx context.Context, key, kind, path string, query url.Values) ([]T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		defaultBaseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(apiKeyHeader, key)

	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the %s list answered status %d", kind, resp.StatusCode)
	}

	var body struct {
		Content []T `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Content, nil
}

// inboxSummary is the part of the inbox list projection the sweeper reads.
type inboxSummary struct {
	ID        string  `json:"id"`
	Name      *string `json:"name"`
	CreatedAt string  `json:"createdAt"`
}

func listRecentInboxes(ctx context.Context, key string, page int) ([]inboxSummary, error) {
	query := pagedQuery(page)
	query.Set("since", sinceTheSweepWindow())
	return listPage[inboxSummary](ctx, key, testInboxKind, inboxesPath+"/paginated", query)
}

// webhookSummary is the part of the webhook list projection the sweeper reads. The projection
// also carries a user name and a password in plain text, which nothing here reads or prints.
type webhookSummary struct {
	ID        string  `json:"id"`
	Name      *string `json:"name"`
	CreatedAt string  `json:"createdAt"`
}

// listWebhooks reads the account-wide webhooks as well as the inbox-scoped ones.
func listWebhooks(ctx context.Context, key string, page int) ([]webhookSummary, error) {
	query := pagedQuery(page)
	query.Set("includeAccountWide", "true")
	return listPage[webhookSummary](ctx, key, testWebhookKind, webhooksPath+"/paginated", query)
}

// rulesetSummary is the part of the ruleset list projection the sweeper reads.
type rulesetSummary struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	CreatedAt string `json:"createdAt"`
}

// listRulesets reads the account rulesets, for every inbox at once.
func listRulesets(ctx context.Context, key string, page int) ([]rulesetSummary, error) {
	return listPage[rulesetSummary](ctx, key, testRulesetKind, rulesetsPath, pagedQuery(page))
}

// templateSummary is the part of the template list projection the sweeper reads. The projection
// reports the variables as bare names, and nothing here reads them.
type templateSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

func listTemplates(ctx context.Context, key string, page int) ([]templateSummary, error) {
	query := pagedQuery(page)
	query.Set("since", sinceTheSweepWindow())
	return listPage[templateSummary](ctx, key, testTemplateKind, templatesPath+"/paginated", query)
}

// forwarderSummary is the part of the forwarder list projection the sweeper reads. The forward
// address list carries the run marker, because the API gives a forwarder no name a program can set.
type forwarderSummary struct {
	ID                  string   `json:"id"`
	ForwardToRecipients []string `json:"forwardToRecipients"`
	CreatedAt           string   `json:"createdAt"`
}

// listForwarders reads the account forwarders, for every inbox at once.
func listForwarders(ctx context.Context, key string, page int) ([]forwarderSummary, error) {
	query := pagedQuery(page)
	query.Set("since", sinceTheSweepWindow())
	return listPage[forwarderSummary](ctx, key, testForwarderKind, forwardersPath, query)
}

// The five sweepers. Each one names its kind's own list reader, marker and stale predicate; the
// sweeps themselves are the shared ones above.
var (
	inboxSweeper = sweeper[inboxSummary]{
		kind:    testInboxKind,
		list:    listRecentInboxes,
		id:      func(s inboxSummary) string { return s.ID },
		markers: func(s inboxSummary) []string { return textOrNone(s.Name) },
		remove: func(ctx context.Context, client Client, id string) error {
			return client.DeleteInbox(ctx, id)
		},
		pattern: testInboxNamePattern,
		stale: func(s inboxSummary, now time.Time) bool {
			createdAt, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err != nil {
				return false
			}
			return isSweepableTestInbox(s.Name, createdAt, now)
		},
	}

	webhookSweeper = sweeper[webhookSummary]{
		kind:    testWebhookKind,
		list:    listWebhooks,
		id:      func(s webhookSummary) string { return s.ID },
		markers: func(s webhookSummary) []string { return textOrNone(s.Name) },
		remove: func(ctx context.Context, client Client, id string) error {
			return client.DeleteWebhook(ctx, id)
		},
		pattern: testWebhookNamePattern,
		stale: func(s webhookSummary, now time.Time) bool {
			createdAt, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err != nil {
				return false
			}
			return isSweepableTestWebhook(s.ID, s.Name, createdAt, now)
		},
	}

	rulesetSweeper = sweeper[rulesetSummary]{
		kind:    testRulesetKind,
		list:    listRulesets,
		id:      func(s rulesetSummary) string { return s.ID },
		markers: func(s rulesetSummary) []string { return []string{s.Target} },
		remove: func(ctx context.Context, client Client, id string) error {
			return client.DeleteRuleset(ctx, id)
		},
		pattern: rulesetTargetPattern,
		stale: func(s rulesetSummary, now time.Time) bool {
			createdAt, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err != nil {
				return false
			}
			return isSweepableTestRuleset(s.ID, s.Target, createdAt, now)
		},
	}

	templateSweeper = sweeper[templateSummary]{
		kind:    testTemplateKind,
		list:    listTemplates,
		id:      func(s templateSummary) string { return s.ID },
		markers: func(s templateSummary) []string { return []string{s.Name} },
		remove: func(ctx context.Context, client Client, id string) error {
			return client.DeleteTemplate(ctx, id)
		},
		pattern: testTemplateNamePattern,
		stale: func(s templateSummary, now time.Time) bool {
			createdAt, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err != nil {
				return false
			}
			return isSweepableTestTemplate(s.ID, s.Name, createdAt, now)
		},
	}

	forwarderSweeper = sweeper[forwarderSummary]{
		kind:    testForwarderKind,
		list:    listForwarders,
		id:      func(s forwarderSummary) string { return s.ID },
		markers: func(s forwarderSummary) []string { return s.ForwardToRecipients },
		remove: func(ctx context.Context, client Client, id string) error {
			return client.DeleteForwarder(ctx, id)
		},
		pattern: forwarderRecipientPattern,
		stale: func(s forwarderSummary, now time.Time) bool {
			createdAt, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err != nil {
				return false
			}
			return isSweepableTestForwarder(s.ID, s.ForwardToRecipients, createdAt, now)
		},
	}
)

// textOrNone answers the one marker a nullable name carries, or none at all.
func textOrNone(name *string) []string {
	if name == nil {
		return nil
	}
	return []string{*name}
}

// staleSweep is one kind's crash-recovery sweep: the sweeper to run, and the plural its line reads.
type staleSweep struct {
	kind   string
	plural string
	run    func(ctx context.Context, key string) int
}

// staleSweeps is every kind's crash-recovery sweep, which the suite runs before its first test. A
// kind missing here keeps the leftovers of a crashed run on the account for good.
func staleSweeps() []staleSweep {
	return []staleSweep{
		{testInboxKind, "inboxes", inboxSweeper.sweepStale},
		{testWebhookKind, "webhooks", webhookSweeper.sweepStale},
		{testRulesetKind, "rulesets", rulesetSweeper.sweepStale},
		{testTemplateKind, "templates", templateSweeper.sweepStale},
		{testForwarderKind, "forwarders", forwarderSweeper.sweepStale},
	}
}

// sweepProbe drives one kind's sweeper from the test tier. Each kind builds its own list entry, so
// every field below is a closure over that kind's summary type.
type sweepProbe struct {
	// newMarker builds a marker of one run, the way the lifecycle tests do.
	newMarker func() string
	// foreign are markers a live account can carry that this package never wrote.
	foreign []string
	// gateOpen reports whether the marked sweep accepts a marker at all.
	gateOpen func(marker string) bool
	// matches reports whether the marked sweep would delete an object carrying itemMarker.
	matches func(marker, itemMarker string) bool
	// stale reports whether the stale sweep would delete that object.
	stale func(itemMarker, createdAt string, now time.Time) bool
}

func sweepProbes() map[string]sweepProbe {
	return map[string]sweepProbe{
		testInboxKind: {
			newMarker: func() string { return newTestName(testInboxKind) },
			foreign: []string{
				"", "a real inbox", "pulumi-test-inbox-ABCD1234",
				testNamePrefix + testWebhookKind + "-abcd1234",
			},
			gateOpen: func(marker string) bool { return inboxSweeper.markedSweepable(marker) != nil },
			matches: func(marker, itemMarker string) bool {
				return inboxSweeper.markedSweepable(marker)(inboxSummary{ID: "id", Name: &itemMarker})
			},
			stale: func(itemMarker, createdAt string, now time.Time) bool {
				return inboxSweeper.stale(
					inboxSummary{ID: "id", Name: &itemMarker, CreatedAt: createdAt}, now)
			},
		},
		testWebhookKind: {
			newMarker: func() string { return newTestName(testWebhookKind) },
			foreign: []string{
				"", "a real webhook", "pulumi-test-webhook-ABCD1234",
				testNamePrefix + testInboxKind + "-abcd1234",
			},
			gateOpen: func(marker string) bool { return webhookSweeper.markedSweepable(marker) != nil },
			matches: func(marker, itemMarker string) bool {
				return webhookSweeper.markedSweepable(marker)(webhookSummary{ID: "id", Name: &itemMarker})
			},
			stale: func(itemMarker, createdAt string, now time.Time) bool {
				return webhookSweeper.stale(
					webhookSummary{ID: "id", Name: &itemMarker, CreatedAt: createdAt}, now)
			},
		},
		testRulesetKind: {
			newMarker: func() string { return rulesetTargetFor(newTestName(testRulesetKind)) },
			foreign: []string{
				"", wildcardTarget, "*@pulumi-test-ruleset-ABCD1234.example.com",
				"*@" + testNamePrefix + testInboxKind + "-abcd1234.example.com",
			},
			gateOpen: func(marker string) bool { return rulesetSweeper.markedSweepable(marker) != nil },
			matches: func(marker, itemMarker string) bool {
				return rulesetSweeper.markedSweepable(marker)(rulesetSummary{ID: "id", Target: itemMarker})
			},
			stale: func(itemMarker, createdAt string, now time.Time) bool {
				return rulesetSweeper.stale(
					rulesetSummary{ID: "id", Target: itemMarker, CreatedAt: createdAt}, now)
			},
		},
		testTemplateKind: {
			newMarker: func() string { return newTestName(testTemplateKind) },
			foreign: []string{
				"", "the welcome letter", "pulumi-test-template-ABCD1234",
				testNamePrefix + testInboxKind + "-abcd1234",
			},
			gateOpen: func(marker string) bool { return templateSweeper.markedSweepable(marker) != nil },
			matches: func(marker, itemMarker string) bool {
				return templateSweeper.markedSweepable(marker)(templateSummary{ID: "id", Name: itemMarker})
			},
			stale: func(itemMarker, createdAt string, now time.Time) bool {
				return templateSweeper.stale(
					templateSummary{ID: "id", Name: itemMarker, CreatedAt: createdAt}, now)
			},
		},
		testForwarderKind: {
			newMarker: func() string { return forwarderRecipientFor(newTestName(testForwarderKind)) },
			foreign: []string{
				"", "forward@example.com", "pulumi-test-forwarder-ABCD1234@example.com",
				testNamePrefix + testInboxKind + "-abcd1234@example.com",
			},
			gateOpen: func(marker string) bool { return forwarderSweeper.markedSweepable(marker) != nil },
			matches: func(marker, itemMarker string) bool {
				return forwarderSweeper.markedSweepable(marker)(
					forwarderSummary{ID: "id", ForwardToRecipients: []string{itemMarker}})
			},
			stale: func(itemMarker, createdAt string, now time.Time) bool {
				return forwarderSweeper.stale(forwarderSummary{
					ID: "id", ForwardToRecipients: []string{itemMarker}, CreatedAt: createdAt}, now)
			},
		},
	}
}

// The marked sweep runs against a live account, so a marker this package never wrote must never
// reach the delete. The gate is the only thing between a mistyped argument and somebody's data.
func TestTheMarkedSweepRefusesEveryMarkerThisPackageDidNotBuild(t *testing.T) {
	t.Parallel()
	for kind, probe := range sweepProbes() {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			for _, marker := range probe.foreign {
				assert.False(t, probe.gateOpen(marker), "the gate opened for %q", marker)
			}
			assert.True(t, probe.gateOpen(probe.newMarker()), "the gate refused a marker of this run")
		})
	}
}

// A kind the gates cover but the startup sweep skips keeps the leftovers of a crashed run on the
// account for good, so the two lists must name the same kinds.
func TestTheStartupSweepRunsForEveryKindTheGatesCover(t *testing.T) {
	t.Parallel()
	var swept []string
	for _, sweep := range staleSweeps() {
		swept = append(swept, sweep.kind)
		assert.NotEqual(t, sweep.kind, sweep.plural, "%s: the line about two objects reads wrong",
			sweep.kind)
	}

	var gated []string
	for kind := range sweepProbes() {
		gated = append(gated, kind)
	}
	assert.ElementsMatch(t, gated, swept)
}

// A marked sweep deletes the objects of its own run and leaves every other run alone, even when
// the other run built its marker the same way.
func TestTheMarkedSweepMatchesTheObjectsOfItsOwnRunAlone(t *testing.T) {
	t.Parallel()
	for kind, probe := range sweepProbes() {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			mine := probe.newMarker()
			assert.True(t, probe.matches(mine, mine), "the sweep must delete its own object")
			assert.False(t, probe.matches(mine, probe.newMarker()),
				"the sweep must leave another run's object alone")
			for _, marker := range probe.foreign {
				assert.False(t, probe.matches(mine, marker), "the sweep must leave %q alone", marker)
			}

			// A live account can hold an object whose marker merely starts or ends with ours.
			for _, near := range []string{mine + " (copy)", mine + "\n", mine + "0", mine[1:],
				mine[:len(mine)-1], " " + mine} {
				assert.False(t, probe.matches(mine, near),
					"a marker that only resembles ours is another owner's: %q", near)
			}
		})
	}
}

// A forwarder carries its marker in the forward address list, so a forwarder that sends anywhere
// else is not ours even when one of its addresses matches.
func TestTheMarkedForwarderSweepRefusesAForwarderThatSendsAnywhereElse(t *testing.T) {
	t.Parallel()
	recipient := forwarderRecipientFor(newTestName(testForwarderKind))
	sweepable := forwarderSweeper.markedSweepable(recipient)
	require.NotNil(t, sweepable)

	assert.True(t, sweepable(forwarderSummary{ID: "id", ForwardToRecipients: []string{recipient}}))
	assert.False(t, sweepable(forwarderSummary{
		ID: "id", ForwardToRecipients: []string{recipient, "audit@example.com"}}),
		"a forwarder that sends anywhere else is not one of ours")
	assert.False(t, sweepable(forwarderSummary{ID: "id"}),
		"a forwarder without a recipient carries no marker")
}

// The stale sweep deletes leftovers of a crashed run. It must leave a young object alone, because
// a concurrent run owns it, and it must leave an object it cannot date alone for the same reason.
func TestTheStaleSweepLeavesAForeignYoungOrUndatedObjectAlone(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	now := old.Add(24 * time.Hour)
	oldStamp := old.Format(time.RFC3339)

	for kind, probe := range sweepProbes() {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			mine := probe.newMarker()
			assert.True(t, probe.stale(mine, oldStamp, now), "a leftover of a crashed run must go")

			assert.False(t, probe.stale(mine, now.Add(-time.Minute).Format(time.RFC3339), now),
				"a fresh object can belong to a concurrent run")
			assert.False(t, probe.stale(mine, "", now),
				"an object this sweep cannot date is never a candidate")
			assert.False(t, probe.stale(mine, "the third of August", now),
				"an object this sweep cannot date is never a candidate")
			for _, marker := range probe.foreign {
				assert.False(t, probe.stale(marker, oldStamp, now), "must not sweep %q", marker)
			}
		})
	}
}

// The sweep must finish the page walk before it deletes anything: a delete shifts the later
// pages, so an entry behind the deleted one never reaches the walk.
func TestTheSweepCollectsEveryPageBeforeItDeletes(t *testing.T) {
	t.Parallel()
	name := testNamePrefix + testWebhookKind + "-abcd1234"
	pages := [][]webhookSummary{
		{{ID: "first", Name: &name}, {ID: "", Name: &name}},
		{{ID: "second", Name: ptr("a real webhook")}, {ID: "third", Name: &name}},
		{},
	}

	var walked []int
	list := func(_ context.Context, _ string, page int) ([]webhookSummary, error) {
		walked = append(walked, page)
		if page >= len(pages) {
			return nil, nil
		}
		return pages[page], nil
	}

	ids := collectSweepableIDs(context.Background(), "unused", testWebhookKind, list,
		func(s webhookSummary) string { return s.ID },
		func(s webhookSummary) bool { return s.Name != nil && *s.Name == name })

	assert.Equal(t, []string{"first", "third"}, ids, "the walk must reach every page")
	assert.Equal(t, []int{0, 1, 2}, walked, "the walk must stop at the first empty page")
}

// The sweep deletes what the walk collected and nothing else, and answers how many it removed. A
// delete that answers "already gone" still counts: the object is off the account either way.
func TestTheSweepDeletesEveryCollectedIdentifierAndCountsThem(t *testing.T) {
	t.Parallel()
	name := testNamePrefix + testWebhookKind + "-abcd1234"
	gone := &APIError{StatusCode: http.StatusNotFound, ErrorCode: errorCodeEntityNotFound}

	const goneID, failedID = "already-gone", "unreachable"
	var deleted []string
	probe := sweeper[webhookSummary]{
		kind: testWebhookKind,
		list: func(_ context.Context, _ string, page int) ([]webhookSummary, error) {
			if page > 0 {
				return nil, nil
			}
			return []webhookSummary{
				{ID: "mine", Name: &name},
				{ID: "theirs", Name: ptr("a real webhook")},
				{ID: goneID, Name: &name},
				{ID: failedID, Name: &name},
			}, nil
		},
		id:      func(s webhookSummary) string { return s.ID },
		markers: func(s webhookSummary) []string { return textOrNone(s.Name) },
		remove: func(_ context.Context, _ Client, id string) error {
			deleted = append(deleted, id)
			switch id {
			case goneID:
				return gone
			case failedID:
				return &APIError{StatusCode: http.StatusInternalServerError}
			}
			return nil
		},
		pattern: testWebhookNamePattern,
	}

	removed := probe.sweepMarked(context.Background(), "unused", name)
	assert.Equal(t, []string{"mine", goneID, failedID}, deleted, "the sweep deleted the wrong objects")
	assert.Equal(t, 2, removed,
		"a delete that answered `already gone` counts, and one that failed for any other reason does not")

	deleted = nil
	assert.Zero(t, probe.sweepMarked(context.Background(), "unused", "a real webhook"),
		"a marker this package never built must reach no delete")
	assert.Empty(t, deleted)
}

// The stale sweep deletes the leftovers of a crashed run and nothing else. It reads the clock, so
// the fixture below dates one object well outside the age gate and one just inside it.
func TestTheStaleSweepDeletesTheLeftoversAndLeavesTheRestAlone(t *testing.T) {
	t.Parallel()
	name := testNamePrefix + testWebhookKind + "-abcd1234"
	old := time.Now().UTC().Add(-2 * sweepMinAge).Format(time.RFC3339)
	fresh := time.Now().UTC().Format(time.RFC3339)

	var deleted []string
	probe := sweeper[webhookSummary]{
		kind: testWebhookKind,
		list: func(_ context.Context, _ string, page int) ([]webhookSummary, error) {
			if page > 0 {
				return nil, nil
			}
			return []webhookSummary{
				{ID: "leftover", Name: &name, CreatedAt: old},
				{ID: "running", Name: &name, CreatedAt: fresh},
				{ID: "theirs", Name: ptr("a real webhook"), CreatedAt: old},
			}, nil
		},
		id:      func(s webhookSummary) string { return s.ID },
		markers: func(s webhookSummary) []string { return textOrNone(s.Name) },
		remove: func(_ context.Context, _ Client, id string) error {
			deleted = append(deleted, id)
			return nil
		},
		pattern: testWebhookNamePattern,
		stale:   webhookSweeper.stale,
	}

	assert.Equal(t, 1, probe.sweepStale(context.Background(), "unused"))
	assert.Equal(t, []string{"leftover"}, deleted,
		"the stale sweep must leave a running run's object and another owner's object alone")
}

// Each sweeper must delete through its own client method. One wired to another kind's delete would
// leave its objects on the account and still report them removed.
func TestEverySweeperDeletesThroughItsOwnClientMethod(t *testing.T) {
	t.Parallel()
	const id = "object-1"
	for kind, tc := range map[string]struct {
		remove func(context.Context, Client, string) error
		expect func(*MockClient)
	}{
		testInboxKind: {inboxSweeper.remove, func(m *MockClient) {
			m.EXPECT().DeleteInbox(gomock.Any(), id).Return(nil)
		}},
		testWebhookKind: {webhookSweeper.remove, func(m *MockClient) {
			m.EXPECT().DeleteWebhook(gomock.Any(), id).Return(nil)
		}},
		testRulesetKind: {rulesetSweeper.remove, func(m *MockClient) {
			m.EXPECT().DeleteRuleset(gomock.Any(), id).Return(nil)
		}},
		testTemplateKind: {templateSweeper.remove, func(m *MockClient) {
			m.EXPECT().DeleteTemplate(gomock.Any(), id).Return(nil)
		}},
		testForwarderKind: {forwarderSweeper.remove, func(m *MockClient) {
			m.EXPECT().DeleteForwarder(gomock.Any(), id).Return(nil)
		}},
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			// The controller fails the test on any call this expectation does not name, and on the
			// named call never happening.
			mock := NewMockClient(gomock.NewController(t))
			tc.expect(mock)
			require.NoError(t, tc.remove(context.Background(), mock, id))
		})
	}
}

// plural renders a count with its noun, so a line about one inbox does not read "1 inboxes".
func plural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

// The budget line and the sweep lines are the only report a run without -v prints, so they must
// read as English at every count a run can reach.
func TestACountReadsAsEnglishAtOneAsWellAsAtNone(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0 inboxes", plural(0, "inbox", "inboxes"))
	assert.Equal(t, "1 inbox", plural(1, "inbox", "inboxes"))
	assert.Equal(t, "3 inboxes", plural(3, "inbox", "inboxes"))
	assert.Equal(t, "1 stale test webhook", plural(1, "stale test webhook", "stale test webhooks"))
}
