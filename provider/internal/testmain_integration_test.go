//go:build integration

package internal

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
)

const (
	//nolint:gosec // G101: this names the environment variable, and holds no credential.
	apiKeyVariable = "MAILSLURP_API_KEY"
	setupTimeout   = 2 * time.Minute

	// cassetteDir holds the recorded calls of this suite. A test runs in the directory of its own
	// package, so the path is relative to it.
	cassetteDir = "testdata/cassettes"

	// replayPlaceholder stands in for the credential of a replay. The scrub drops the key header
	// from every cassette and the matcher reads no header, so the value is never the real one.
	replayPlaceholder = "replay"

	// sharedInboxCassette names the cassette the shared inbox belongs to. Whichever test asks for
	// the inbox first causes the create, so those calls belong to the suite and not to that test.
	sharedInboxCassette = "TestMain_shared_inbox"
)

// countingClientF answers the client factory the integration tests configure the provider with.
// Every call the provider makes goes through the transport of the test, and each inbox it creates
// is charged against the budget.
func countingClientF(transport http.RoundTripper) clientF {
	return func(_ context.Context, cfg *Config) (Client, error) {
		base, err := NewClientWith(cfg.Endpoint, cfg.APIKey, transport)
		if err != nil {
			return nil, err
		}
		return countingClient{Client: base}, nil
	}
}

// theVendorTransport is the transport the process starts with. A test points the default transport
// at its own recorder while it runs, so a call that belongs to the suite rather than to a test names
// this one and stays out of every cassette.
var theVendorTransport = http.DefaultTransport

// theVendorClient answers the client of the sweeps and the teardown of the suite. Those calls
// belong to no test, no cassette holds them, and they run in the record mode and the live mode
// alone, so they name the transport of the process rather than the recorder of a test.
func theVendorClient() *http.Client { return replaytest.ClientFor(theVendorTransport) }

// theFixture builds the recorder of one test. Every call of that test goes through it, and a
// missing cassette fails the test here rather than reaching the account.
func theFixture(t *testing.T) *replaytest.Fixture {
	t.Helper()
	fx := replaytest.New(t, cassetteDir)
	captureTheDefaultTransport(t, fx)
	return fx
}

// captureTheDefaultTransport points the default transport at the recorder of one test, and puts the
// old one back when the test ends. The paged list readers and the sweepers each build an HTTP client
// that names no transport, so they reach the default one, and this is the seam that covers them. The
// tests that reach the account run one after another, so no two of them hold the default at once.
func captureTheDefaultTransport(t *testing.T, fx *replaytest.Fixture) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = fx.Transport()
	t.Cleanup(func() { http.DefaultTransport = previous })
}

// theClient builds a MailSlurp client that sends every call through the recorder of one test.
func theClient(t *testing.T, fx *replaytest.Fixture, key string) Client {
	t.Helper()
	client, err := NewClientWith(defaultBaseURL, key, fx.Transport())
	require.NoError(t, err)
	return client
}

// recordedName builds a vendor object name from the seed of one test. The Nth name of a replay is
// the name the recording made, so a recorded request body still matches. It builds the same shape
// newTestName builds, so every sweep pattern matches it.
func recordedName(fx *replaytest.Fixture, kind string) string {
	return testNamePrefix + kind + "-" + fx.Seed().Hex(4)
}

// sharedInbox is the one inbox every inbox-scoped test reuses. MailSlurp bills each inbox and burns
// a 30-day quota slot, so it is built the first time a test asks and a run that never asks pays none.
var sharedInbox struct {
	once sync.Once
	id   string
	// name is set before the create, so a create the vendor performed and then reported as a
	// failure is still swept in teardown.
	name string
	err  error
}

// theSharedInbox answers the shared inbox, building it on first use.
func theSharedInbox(t *testing.T) string {
	t.Helper()
	key := requireAPIKey(t)
	mode := replaytest.ModeOf(t)
	sharedInbox.once.Do(func() {
		sharedInbox.name, sharedInbox.id, sharedInbox.err = buildTheSharedInbox(mode, key, cassetteDir)
	})
	require.NoError(t, sharedInbox.err, "the suite could not create the shared inbox")
	require.NotEmpty(t, sharedInbox.id, "the create of the shared inbox answered no identifier")
	return sharedInbox.id
}

// theSharedInboxName answers the vendor name of the shared inbox, and builds it when it has to.
func theSharedInboxName(t *testing.T) string {
	t.Helper()
	theSharedInbox(t)
	return sharedInbox.name
}

// theSharedInboxFixture holds the recorder of the cassette the shared inbox belongs to. A run that
// asked for no inbox builds none, and the suite then closes nothing.
var theSharedInboxFixture *replaytest.Fixture

// buildTheSharedInbox creates the one inbox the suite shares. The create belongs to the suite and
// not to whichever test asked for the inbox first, so it goes through a cassette of its own. A
// record run sends it through the transport of the process, so it stays out of the cassette of that
// test, and a replay reads the cassette of the suite and reaches no account.
func buildTheSharedInbox(mode replaytest.Mode, key, dir string) (name, id string, err error) {
	fx, err := replaytest.NewFor(mode, dir, sharedInboxCassette, theVendorTransport)
	if err != nil {
		return "", "", err
	}
	theSharedInboxFixture = fx

	client, err := NewClientWith(defaultBaseURL, key, fx.Transport())
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	// The name comes from the seed of that cassette, so a replay sends the body the recording
	// holds. The suite draws the name before the create, so a create the vendor performed and then
	// reported as a failure still leaves a name the teardown sweeps.
	name = recordedName(fx, testInboxKind)
	id, err = createTheSharedInbox(ctx, client, name)
	return name, id, err
}

// stopTheSharedInboxRecorder closes the cassette of the shared inbox after the last test. It reads
// the recorder after the run, because a test builds it while the run is going on.
func stopTheSharedInboxRecorder(code int) int { return closeTheCassette(theSharedInboxFixture, code) }

// closeTheCassette closes one recorder and answers the code the suite reports. A record run writes
// the file here, and a replay reports a recorded call the suite never sent. A run that asked for no
// inbox built no recorder, so it closes nothing.
func closeTheCassette(fx *replaytest.Fixture, code int) int {
	if fx == nil {
		return code
	}
	if err := fx.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "the cassette of the shared inbox failed: %v\n", err)
		return 1
	}
	return code
}

// A replay of the shared inbox reads a cassette of its own, so it reaches no account. A cassette
// nobody recorded names the file and the target that writes it, the same way a test does.
func TestTheSharedInboxReplaysACassetteOfItsOwn(t *testing.T) {
	t.Parallel()

	name, id, err := buildTheSharedInbox(replaytest.Replay, replayPlaceholder, t.TempDir())
	require.Error(t, err, "a replay must never create the shared inbox")
	require.ErrorContains(t, err, sharedInboxCassette+".yaml",
		"the refusal must name the cassette the suite reads")
	require.ErrorContains(t, err, "make record_integration",
		"the refusal must name the target that writes it")
	assert.Empty(t, id)
	assert.Empty(t, name)
}

// The suite closes the cassette of the shared inbox after the last test, and a record run writes
// the file at that close.
func TestClosingTheCassetteOfTheSharedInboxWritesIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	assert.Equal(t, 7, closeTheCassette(nil, 7), "a run that built no recorder closes nothing")

	fx, err := replaytest.NewFor(replaytest.Record, dir, sharedInboxCassette, theVendorTransport)
	require.NoError(t, err)
	assert.Zero(t, closeTheCassette(fx, 0))
	assert.FileExists(t, filepath.Join(dir, sharedInboxCassette+".yaml"),
		"the close writes the cassette the suite records")
}

// toTheFake sends every call to a local server. It routes a copy, so the request the recorder
// writes still names the vendor host and a replay matches it.
type toTheFake struct{ address *url.URL }

func (f toTheFake) RoundTrip(r *http.Request) (*http.Response, error) {
	routed := r.Clone(r.Context())
	routed.URL.Scheme = f.address.Scheme
	routed.URL.Host = f.address.Host
	routed.Host = f.address.Host
	return http.DefaultTransport.RoundTrip(routed)
}

// inboxPageFake answers one page of the inbox list, the way the paginated endpoint does.
func inboxPageFake(t *testing.T, name string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = io.WriteString(w, `{"content":[{"id":"`+testInboxID+`","name":"`+name+
			`","createdAt":"2026-01-01T00:00:00Z"}]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// Every list reader sends through the client its caller hands it. A reader that built a client of
// its own would reach the account on a replay, so this replay hands it a transport that refuses
// every call and the cassette still answers.
func TestAReplayedListReadsTheCassetteThroughTheClientOfTheTest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const name = testNamePrefix + testInboxKind + "-abcd1234"
	const cassetteName = "TestAReplayedList"

	server := inboxPageFake(t, name)
	address, err := url.Parse(server.URL)
	require.NoError(t, err)

	recording, err := replaytest.NewFor(replaytest.Record, dir, cassetteName, toTheFake{address})
	require.NoError(t, err)
	found, err := listRecentInboxes(t.Context(), recording.Client(), replayPlaceholder, 0)
	require.NoError(t, err)
	require.Len(t, found, 1, "the recorded read must answer the page the vendor sent")
	require.NoError(t, recording.Stop())
	server.Close()

	// The `since` value moves with the clock, so the replayed request never carries the recorded
	// one. The matcher drops it, and the cassette answers the read with the vendor stopped.
	replaying, err := replaytest.NewFor(replaytest.Replay, dir, cassetteName, refusingTransport{})
	require.NoError(t, err)
	replayed, err := listRecentInboxes(t.Context(), replaying.Client(), replayPlaceholder, 0)
	require.NoError(t, err, "the reader must answer from the cassette and reach no account")
	require.Equal(t, found, replayed)
	require.NoError(t, replaying.Stop(), "the replay must consume the recorded read")
	require.Len(t, replaying.Requests(), 1, "the read must go through the recorder of the caller")
}

// accountSnapshot is the read-only account state the zero-mutation proof compares.
type accountSnapshot struct {
	Domains  []string
	Inboxes  []string
	Webhooks []string
}

// sortedIDs answers the identifiers of one list projection in a stable order, so two snapshots of
// an unchanged account compare equal.
func sortedIDs[T any](items []T, identify func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, identify(item))
	}
	sort.Strings(out)
	return out
}

// readAccount answers the identifiers the account holds. The inbox and webhook lists carry the
// page and age bounds the sweeper uses, which reach every object a test run of this suite builds.
func readAccount(t *testing.T, fx *replaytest.Fixture, key string) accountSnapshot {
	t.Helper()
	ctx := context.Background()

	domains, err := listDomains(ctx, fx.Client(), key)
	require.NoError(t, err)
	inboxes, err := listRecentInboxes(ctx, fx.Client(), key, 0)
	require.NoError(t, err)
	webhooks, err := listWebhooks(ctx, fx.Client(), key, 0)
	require.NoError(t, err)

	return accountSnapshot{
		Domains:  sortedIDs(domains, func(s domainSummary) string { return s.ID }),
		Inboxes:  sortedIDs(inboxes, func(s inboxSummary) string { return s.ID }),
		Webhooks: sortedIDs(webhooks, func(s webhookSummary) string { return s.ID }),
	}
}

func apiKey() string { return os.Getenv(apiKeyVariable) }

// requireAPIKey answers the key this run needs. A replay reads a cassette and reaches no account,
// so it takes a placeholder and never skips. A record run and a live run both reach the account,
// so they need the credential, and they skip when it is absent.
func requireAPIKey(t *testing.T) string {
	t.Helper()
	if replaytest.ModeOf(t) == replaytest.Replay {
		return replayPlaceholder
	}
	key := apiKey()
	if key == "" {
		t.Skipf("set %s to run the integration tests", apiKeyVariable)
	}
	return key
}

// countWebhooks answers how many webhooks the account holds, up to the page bound of the sweep.
func countWebhooks(ctx context.Context, httpClient *http.Client, key string) (int, error) {
	total := 0
	for page := range sweepMaxPages {
		found, err := listWebhooks(ctx, httpClient, key, page)
		if err != nil {
			return 0, err
		}
		if len(found) == 0 {
			return total, nil
		}
		total += len(found)
	}
	return total, nil
}

// removeTheSharedInbox clears the shared inbox after the run. It deletes the identifier the create
// answered, then sweeps the name: a retried create can leave a second inbox this process never saw.
func removeTheSharedInbox(
	id, name string,
	remove func(string) error,
	sweep func(string) int,
) error {
	var failure error
	if id != "" {
		if err := remove(id); err != nil && !IsGone(err) {
			failure = err
		}
	}
	if name != "" {
		sweep(name)
	}
	return failure
}

// The teardown must clear the shared inbox by name as well as by identifier. A create that the
// vendor performed and then reported as a failure, or a retried create, leaves an inbox behind.
func TestTheTeardownClearsTheSharedInboxByNameAsWellAsByIdentifier(t *testing.T) {
	t.Parallel()
	name := newTestName(testInboxKind)

	for _, tc := range []struct {
		title      string
		id         string
		wantDelete []string
	}{
		{"the create answered an identifier", "inbox-1", []string{"inbox-1"}},
		{"the create answered an error, so no identifier exists", "", nil},
	} {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			var deleted, swept []string
			err := removeTheSharedInbox(tc.id, name,
				func(id string) error { deleted = append(deleted, id); return nil },
				func(n string) int { swept = append(swept, n); return 0 })
			require.NoError(t, err)
			assert.Equal(t, tc.wantDelete, deleted,
				"a blank identifier reaches the whole collection, so it is never deleted")
			assert.Equal(t, []string{name}, swept, "the shared name must always be swept")
		})
	}
}

// A delete that answers "already gone" is a success: the sweep still runs, and the run still passes.
func TestTheTeardownReportsADeleteThatFailedForAnyOtherReason(t *testing.T) {
	t.Parallel()
	name := newTestName(testInboxKind)
	gone := &APIError{StatusCode: http.StatusNotFound, ErrorCode: errorCodeEntityNotFound}
	other := &APIError{StatusCode: http.StatusInternalServerError}

	var swept int
	sweep := func(string) int { swept++; return 0 }

	require.NoError(t, removeTheSharedInbox("inbox-1", name,
		func(string) error { return gone }, sweep))
	require.Error(t, removeTheSharedInbox("inbox-1", name,
		func(string) error { return other }, sweep))
	assert.Equal(t, 2, swept, "a failed delete must not stop the sweep")
}

func TestMain(m *testing.M) { os.Exit(runIntegrationSuite(m)) }

// listingOnly reports whether this run only prints test names. The testing package handles
// -list inside m.Run, so setup that spends billed vendor quota must not run ahead of it.
func listingOnly() bool {
	flag.Parse()
	f := flag.Lookup("test.list")
	return f != nil && f.Value.String() != ""
}

func runIntegrationSuite(m *testing.M) int {
	mode, err := replaytest.ReadMode()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	key := apiKey()
	// A listing run prints test names only, so it builds no vendor object and it needs no setup.
	if listingOnly() {
		return m.Run()
	}
	// A replay reads a cassette and reaches no account, so it sweeps nothing and deletes nothing.
	if mode == replaytest.Replay {
		fmt.Fprintln(os.Stderr, "the suite replays cassettes, so it sweeps no account")
		return stopTheSharedInboxRecorder(m.Run())
	}
	// An absent key makes every test skip, and a run that builds no vendor object needs no sweep.
	if key == "" {
		return stopTheSharedInboxRecorder(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	teardown := context.WithoutCancel(ctx)

	// A crashed run never reaches its cleanup, so every kind is swept before the first test.
	for _, sweep := range staleSweeps() {
		if removed := sweep.run(ctx, theVendorClient(), key); removed > 0 {
			fmt.Fprintf(os.Stderr, "the sweeper removed %s\n",
				plural(removed, "stale test "+sweep.kind, "stale test "+sweep.plural))
		}
	}

	code := stopTheSharedInboxRecorder(m.Run())

	// The shared inbox exists only when a test asked for it, and the teardown tolerates an inbox
	// that is already gone.
	if err := removeTheSharedInbox(sharedInbox.id, sharedInbox.name,
		func(id string) error {
			client, err := NewClient(defaultBaseURL, key)
			if err != nil {
				return err
			}
			return client.DeleteInbox(teardown, id)
		},
		func(name string) int {
			return inboxSweeper.sweepMarked(teardown, theVendorClient(), key, name)
		},
	); err != nil {
		fmt.Fprintf(os.Stderr, "the suite could not delete the shared inbox: %v\n", err)
		code = 1
	}

	spent := inboxCreations.Load()
	fmt.Fprintf(os.Stderr, "the suite created %s\n", plural(int(spent), "inbox", "inboxes"))
	if spent > maxInboxCreations {
		fmt.Fprintf(os.Stderr, "the suite exceeded the budget of %d inboxes\n", maxInboxCreations)
		code = 1
	}
	return code
}
