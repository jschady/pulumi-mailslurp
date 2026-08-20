//go:build integration

package internal

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	//nolint:gosec // G101: this names the environment variable, and holds no credential.
	apiKeyVariable = "MAILSLURP_API_KEY"
	setupTimeout   = 2 * time.Minute
)

// countingClientF is the client factory the integration tests configure the provider with.
func countingClientF(ctx context.Context, cfg *Config) (Client, error) {
	base, err := RealClientF(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return countingClient{Client: base}, nil
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
	sharedInbox.once.Do(func() { buildTheSharedInbox(key) })
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

func buildTheSharedInbox(key string) {
	client, err := NewClient(defaultBaseURL, key)
	if err != nil {
		sharedInbox.err = err
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	sharedInbox.id, sharedInbox.name, sharedInbox.err = createTheSharedInbox(ctx, client)
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
func readAccount(t *testing.T, key string) accountSnapshot {
	t.Helper()
	ctx := context.Background()

	domains, err := listDomains(ctx, key)
	require.NoError(t, err)
	inboxes, err := listRecentInboxes(ctx, key, 0)
	require.NoError(t, err)
	webhooks, err := listWebhooks(ctx, key, 0)
	require.NoError(t, err)

	return accountSnapshot{
		Domains:  sortedIDs(domains, func(s domainSummary) string { return s.ID }),
		Inboxes:  sortedIDs(inboxes, func(s inboxSummary) string { return s.ID }),
		Webhooks: sortedIDs(webhooks, func(s webhookSummary) string { return s.ID }),
	}
}

func apiKey() string { return os.Getenv(apiKeyVariable) }

// requireAPIKey skips the calling test when the credential is absent, and names the variable.
func requireAPIKey(t *testing.T) string {
	t.Helper()
	key := apiKey()
	if key == "" {
		t.Skipf("set %s to run the integration tests", apiKeyVariable)
	}
	return key
}

// countWebhooks answers how many webhooks the account holds, up to the page bound of the sweep.
func countWebhooks(ctx context.Context, key string) (int, error) {
	total := 0
	for page := range sweepMaxPages {
		found, err := listWebhooks(ctx, key, page)
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
	key := apiKey()
	// A listing run prints test names only, and an absent key makes every test skip. Neither
	// builds a vendor object, so neither runs the setup below.
	if listingOnly() || key == "" {
		return m.Run()
	}

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	teardown := context.WithoutCancel(ctx)

	// A crashed run never reaches its cleanup, so every kind is swept before the first test.
	for _, sweep := range staleSweeps() {
		if removed := sweep.run(ctx, key); removed > 0 {
			fmt.Fprintf(os.Stderr, "the sweeper removed %s\n",
				plural(removed, "stale test "+sweep.kind, "stale test "+sweep.plural))
		}
	}

	code := m.Run()

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
		func(name string) int { return inboxSweeper.sweepMarked(teardown, key, name) },
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
