//go:build integration

package internal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// forwarderIDsSendingTo answers the identifiers of the forwarders that send to one address. The
// lifecycle test reads it to prove that the update kept the one forwarder the create built.
func forwarderIDsSendingTo(ctx context.Context, key, recipient string) ([]string, error) {
	var ids []string
	for page := range sweepMaxPages {
		found, err := listForwarders(ctx, key, page)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return ids, nil
		}
		for _, summary := range found {
			if summary.ID == "" {
				continue
			}
			for _, address := range summary.ForwardToRecipients {
				if address == recipient {
					ids = append(ids, summary.ID)
					break
				}
			}
		}
	}
	return ids, nil
}

// recipientsFrom reads the forward address list of one lifecycle answer.
func recipientsFrom(output property.Map) []string {
	list := output.Get(forwarderPropForwardToRecipients).AsArray()

	out := make([]string, 0, list.Len())
	for i := range list.Len() {
		out = append(out, list.Get(i).AsString())
	}
	return out
}

// requireForwarderEntitlement skips the calling test when the account plan refuses a forwarder.
// MailSlurp gates the create alone, so no read answers the question and the probe has to write.
func requireForwarderEntitlement(t *testing.T, key, inboxID string) {
	t.Helper()
	client, err := NewClient(defaultBaseURL, key)
	require.NoError(t, err)

	ctx := context.Background()
	recipient := forwarderRecipientFor(newTestName(testForwarderKind))
	// The sweep is registered before the create, so a create the vendor performed and then reported
	// as a failure is still removed.
	t.Cleanup(func() {
		forwarderSweeper.sweepMarked(context.WithoutCancel(ctx), key, recipient)
	})

	// The API refuses a body that names neither a field and a match nor a match tree, so the probe
	// sends the same shape the lifecycle sends. Probed live on 2026-08-19.
	created, err := client.CreateForwarder(ctx, inboxID, ForwarderOptions{
		Field:               ptr(string(ForwarderFieldSubject)),
		Match:               ptr(testForwarderMatch),
		ForwardToRecipients: []string{recipient},
	})
	if reason := skipReasonForForwarderEntitlement(err); reason != "" {
		t.Skip(reason)
	}
	require.NoError(t, err, "the entitlement probe failed for another reason")

	require.NotEmpty(t, created.ID, "the entitlement probe captured no identifier")
	require.NoError(t, client.DeleteForwarder(ctx, created.ID))
}

// TestInboxForwarderLifeCycle creates zero inboxes of its own: every forwarder lands on the shared
// inbox. The `match` change proves the in-place update path.
func TestInboxForwarderLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	guardTheInboxBudget(t)
	requireForwarderEntitlement(t, key, sharedInboxID)

	s := configuredServer(t, key)
	recipient := forwarderRecipientFor(newTestName(testForwarderKind))

	// Cleanup runs on failure too. It deletes by an identifier the list answered, never by a value
	// it did not capture, and it tolerates a forwarder that is already gone.
	t.Cleanup(func() {
		forwarderSweeper.sweepMarked(context.WithoutCancel(context.Background()), key, recipient)
	})

	inputs := func(match string) property.Map {
		return property.NewMap(map[string]property.Value{
			forwarderPropInboxID:             property.New(sharedInboxID),
			forwarderPropForwardToRecipients: strs(recipient),
			forwarderPropField:               property.New(string(ForwarderFieldSubject)),
			forwarderPropShould:              property.New(string(ForwarderShouldWildcard)),
			forwarderPropMatch:               property.New(match),
		})
	}

	var firstID, createdAt string
	var updated bool

	integration.LifeCycleTest{
		Resource: tokens.Type(forwarderResourceType),
		Create: integration.Operation{
			Inputs: inputs(testForwarderMatch),
			Hook: func(_, output property.Map) {
				assert.Equal(t, sharedInboxID, output.Get(forwarderPropInboxID).AsString(),
					"the create must answer the inbox that the query parameter named")
				assert.Equal(t, testForwarderMatch, output.Get(forwarderPropMatch).AsString())
				assert.Equal(t, string(ForwarderFieldSubject),
					output.Get(forwarderPropField).AsString())
				assert.Equal(t, string(ForwarderShouldWildcard),
					output.Get(forwarderPropShould).AsString())
				assert.Equal(t, []string{recipient}, recipientsFrom(output))

				createdAt = output.Get(forwarderPropCreatedAt).AsString()
				_, err := time.Parse(time.RFC3339, createdAt)
				assert.NoError(t, err, "the creation time must parse as RFC 3339: %q", createdAt)

				ids, err := forwarderIDsSendingTo(context.Background(), key, recipient)
				require.NoError(t, err)
				require.Len(t, ids, 1, "the create must build exactly one forwarder")
				firstID = ids[0]
			},
		},
		Updates: []integration.Operation{
			{
				// The match changes. The API takes the whole create body on a PUT to the forwarder
				// itself, so the update keeps the forwarder rather than building a second one.
				Inputs: inputs(testMatchNext),
				Hook: func(_, output property.Map) {
					updated = true
					assert.Equal(t, testMatchNext, output.Get(forwarderPropMatch).AsString())
					assert.Equal(t, string(ForwarderShouldWildcard),
						output.Get(forwarderPropShould).AsString())
					assert.Equal(t, []string{recipient}, recipientsFrom(output))
					assert.Equal(t, createdAt, output.Get(forwarderPropCreatedAt).AsString(),
						"an in-place update never moves the creation time")

					ids, err := forwarderIDsSendingTo(context.Background(), key, recipient)
					require.NoError(t, err)
					require.NotEmpty(t, firstID, "the create captured no identifier")
					assert.Equal(t, []string{firstID}, ids,
						"the update must keep the forwarder the create built, and build no second one")
				},
			},
		},
	}.Run(t, s)

	// A diff that reports no change makes the harness skip the whole update leg, and every
	// assertion above runs inside it (pulumi-go-provider integration/integration.go:434).
	require.True(t, updated, "the update leg never ran, so nothing proved the in-place update")

	// The lifecycle deletes the forwarder it created, so the account holds none of this run.
	ids, err := forwarderIDsSendingTo(context.Background(), key, recipient)
	require.NoError(t, err)
	assert.Empty(t, ids, "the lifecycle left a forwarder behind")
}

// The suite must leave the account as it found it, and the shared inbox goes with it in teardown.
func TestTheAccountHoldsNoTestForwarderAfterTheLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)

	found, err := listForwarders(context.Background(), key, 0)
	require.NoError(t, err)
	for _, summary := range found {
		for _, address := range summary.ForwardToRecipients {
			assert.False(t, forwarderRecipientPattern.MatchString(address),
				"the forwarder tests left %s behind", summary.ID)
		}
	}
}
