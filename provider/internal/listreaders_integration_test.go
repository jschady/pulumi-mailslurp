//go:build integration

package internal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live proof that every list reader still reads its own endpoint, which an empty account
// cannot give: it answers an empty page for a wrong query as readily as for a right one.

// One request per kind. A path the API does not serve answers a 404 and an unexpected page shape
// fails to decode, so either mistake fails here. This test creates no inbox.
func TestEveryListReaderReachesItsOwnEndpoint(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)

	ctx := context.Background()
	for _, probe := range listReaderProbes() {
		t.Run(probe.kind, func(t *testing.T) {
			require.NoErrorf(t, probe.read(ctx, key),
				"the %s reader should reach the endpoint it builds", probe.kind)
		})
	}
}

// The sweeps of this package delete account webhooks as well as inbox webhooks, and the account
// ones appear only when the reader asks for them. This test creates no inbox.
func TestTheWebhookReaderSeesAnAccountWebhook(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)

	client, err := NewClient(defaultBaseURL, key)
	require.NoError(t, err)

	ctx := context.Background()
	name := newTestName(testWebhookKind)
	// The sweep is registered before the create, so a webhook the API made and then reported as a
	// failure is still removed.
	t.Cleanup(func() { webhookSweeper.sweepMarked(context.WithoutCancel(ctx), key, name) })

	created, err := client.CreateAccountWebhook(ctx, WebhookOptions{
		URL:       "https://example.com/mailslurp/list-reader",
		EventName: string(WebhookEventNewEmail),
		Name:      name,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID, "the create answered no identifier")
	t.Cleanup(func() {
		assert.NoError(t, client.DeleteWebhook(context.WithoutCancel(ctx), created.ID))
	})

	found, err := listWebhooks(ctx, key, 0)
	require.NoError(t, err)

	var seen bool
	for _, summary := range found {
		if summary.ID == created.ID {
			seen = true
		}
	}
	assert.True(t, seen, "the reader should list the account webhook it just created")
}
