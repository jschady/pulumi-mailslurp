package internal

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// The inbox budget both test tiers read, so this file carries no build tag. MailSlurp bills every
// inbox and burns a 30-day quota slot a delete never refunds, so the count is a gate, not a report.

// maxInboxCreations is the budget of a full run: the shared inbox, the create assertion and the
// replace assertion. Each one is billed and burns a 30-day quota a delete does not refund.
const maxInboxCreations = 3

// inboxCreations counts every inbox this package builds, through the provider or directly.
var inboxCreations atomic.Int64

// countingClient charges each created inbox against the budget. It wraps the real client, so an
// accidental extra create in any lifecycle method shows up as a failed run, not as a silent bill.
type countingClient struct {
	Client
}

// chargeInbox reserves one inbox against the budget and refuses when the budget is spent. The
// refusal happens before the vendor call, so an overrun costs a failed run and not a billed inbox.
func chargeInbox() error {
	if spent := inboxCreations.Add(1); spent > maxInboxCreations {
		return fmt.Errorf("the inbox budget of %d is spent, and this create is number %d",
			maxInboxCreations, spent)
	}
	return nil
}

func (c countingClient) CreateInbox(ctx context.Context, opts CreateInboxOptions) (*InboxDto, error) {
	if err := chargeInbox(); err != nil {
		return nil, err
	}
	return c.Client.CreateInbox(ctx, opts)
}

// sharedInboxCreations counts the shared inbox alone. Every inbox-scoped test may be the one that
// causes it, and none of them owns it, so the budget guard below leaves it out of a test's own count.
var sharedInboxCreations atomic.Int64

// createTheSharedInbox names the inbox before it asks for it, so a create the vendor performed and
// then reported as a failure still answers a name the teardown can sweep.
func createTheSharedInbox(ctx context.Context, client Client) (id, name string, err error) {
	name = newTestName(testInboxKind)
	// The create goes through the budget gate, and against the suite rather than the caller.
	sharedInboxCreations.Add(1)
	created, err := countingClient{Client: client}.CreateInbox(ctx, CreateInboxOptions{
		Name:        ptr(name),
		Description: ptr("The shared inbox of the pulumi-mailslurp integration suite."),
		Tags:        []string{testNamePrefix + "shared"},
	})
	if err != nil {
		return "", name, err
	}
	return created.ID, name, nil
}

// ownCreations counts the inboxes a test built for itself. The shared inbox belongs to the suite,
// so whichever test happens to ask for it first is not charged for it.
func ownCreations() int64 { return inboxCreations.Load() - sharedInboxCreations.Load() }

// guardTheInboxBudget fails the calling test if it creates an inbox of its own. Every resource but
// the Inbox works on objects that cost nothing, so the count must stand where the test found it.
func guardTheInboxBudget(t *testing.T) {
	t.Helper()
	before := ownCreations()
	t.Cleanup(func() {
		assert.Equal(t, before, ownCreations(), "this test must create no inbox")
	})
}

// The budget must stop the vendor call. A count that only reports the overrun after the run tells
// you about a billed inbox you already paid for.
func TestTheBudgetGateRefusesACreateBeyondTheBudget(t *testing.T) {
	spent := inboxCreations.Load()
	t.Cleanup(func() { inboxCreations.Store(spent) })
	inboxCreations.Store(maxInboxCreations)

	// A mock with no expectation fails the test if the gate lets the create reach the API.
	mock := NewMockClient(gomock.NewController(t))
	_, err := countingClient{Client: mock}.CreateInbox(context.Background(), CreateInboxOptions{})
	require.Error(t, err, "the gate must refuse a create beyond the budget")
	assert.Contains(t, err.Error(), strconv.Itoa(maxInboxCreations))
}

// resetTheInboxCounters gives one test a clean budget and puts the run's own counts back after it.
func resetTheInboxCounters(t *testing.T) {
	t.Helper()
	spent, shared := inboxCreations.Load(), sharedInboxCreations.Load()
	t.Cleanup(func() { inboxCreations.Store(spent); sharedInboxCreations.Store(shared) })
	inboxCreations.Store(0)
	sharedInboxCreations.Store(0)
}

// The shared inbox belongs to the suite, and any test may be the one that causes it. Charging it to
// that test would fail a budget guard for work the test never did.
func TestTheSharedInboxIsNotChargedToTheTestThatAsksForItFirst(t *testing.T) {
	resetTheInboxCounters(t)

	// Building the shared inbox bumps both counters, so a test's own count does not move.
	sharedInboxCreations.Add(1)
	require.NoError(t, chargeInbox())
	assert.Zero(t, ownCreations(), "the shared inbox is the suite's, not the first caller's")

	// Every other create bumps the total alone, so the guard sees it.
	require.NoError(t, chargeInbox())
	assert.EqualValues(t, 1, ownCreations(), "a test's own inbox must count against it")
	assert.EqualValues(t, 2, inboxCreations.Load(), "both creates count against the budget")
}

// The guard reads the count a test owns, so it holds whichever way a test orders its setup. A
// guard reading the whole budget would fail the test that happens to build the shared inbox.
func TestTheBudgetGuardHoldsWhenTheSharedInboxIsBuiltInsideTheTest(t *testing.T) {
	resetTheInboxCounters(t)

	// The subtest guards first and builds the suite's inbox afterwards, which is the order a test
	// takes when it calls the guard before it asks for the shared inbox. A charged guard fails here.
	t.Run("the test that builds the shared inbox", func(t *testing.T) {
		guardTheInboxBudget(t)
		sharedInboxCreations.Add(1)
		require.NoError(t, chargeInbox())
	})
}

// The shared inbox is billed like any other, so it counts against the budget. It belongs to the
// suite, so it must not count against whichever test happened to ask for it first.
func TestTheSharedInboxCreateChargesTheBudgetAndNotTheCaller(t *testing.T) {
	resetTheInboxCounters(t)

	mock := NewMockClient(gomock.NewController(t))
	var got CreateInboxOptions
	mock.EXPECT().CreateInbox(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts CreateInboxOptions) (*InboxDto, error) {
			got = opts
			return &InboxDto{ID: testInboxID}, nil
		})

	id, name, err := createTheSharedInbox(context.Background(), mock)
	require.NoError(t, err)
	assert.Equal(t, testInboxID, id)
	assert.True(t, testInboxNamePattern.MatchString(name), "the sweeper matches this name: %q", name)
	require.NotNil(t, got.Name)
	assert.Equal(t, name, *got.Name, "the create must carry the name the teardown sweeps")

	assert.EqualValues(t, 1, inboxCreations.Load(), "the shared inbox is billed like any other")
	assert.Zero(t, ownCreations(), "and it belongs to the suite, not to its first caller")
}

// A create the vendor performed and then reported as a failure leaves an inbox behind, so the name
// must survive the failure for the teardown to sweep it.
func TestTheSharedInboxKeepsItsNameWhenTheCreateFails(t *testing.T) {
	resetTheInboxCounters(t)

	mock := NewMockClient(gomock.NewController(t))
	mock.EXPECT().CreateInbox(gomock.Any(), gomock.Any()).
		Return(nil, &APIError{StatusCode: http.StatusInternalServerError})

	id, name, err := createTheSharedInbox(context.Background(), mock)
	require.Error(t, err)
	assert.Empty(t, id, "a create that failed answers no identifier, and a blank one deletes nothing")
	assert.True(t, testInboxNamePattern.MatchString(name),
		"the teardown sweeps this name, so a create the vendor performed is still removed: %q", name)
}
