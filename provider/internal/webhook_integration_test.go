//go:build integration

package internal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// Every webhook target below is unroutable by design: the host is reserved for documentation and
// the random path keeps two runs apart. MailSlurp posts nothing to it during the run.
const lifecycleWebhookHost = "https://example.com/"

// The webhook tests own the account for the run, and they start from a clean slate.
func TestTheAccountHoldsNoWebhooks(t *testing.T) {
	key := requireAPIKey(t)
	count, err := countWebhooks(context.Background(), key)
	require.NoError(t, err)
	assert.Zero(t, count, "the account must hold no webhook before the webhook tests run")
}

// deleteWebhookAfterTheTest removes one webhook by the identifier the create answered. It runs on
// failure too, and it tolerates a webhook that is already gone.
func deleteWebhookAfterTheTest(t *testing.T, client Client, id string) {
	t.Helper()
	t.Cleanup(func() {
		if id == "" {
			t.Error("no webhook identifier was captured, so nothing was deleted")
			return
		}
		if err := client.DeleteWebhook(context.WithoutCancel(context.Background()), id); err != nil &&
			!IsGone(err) {
			t.Errorf("the cleanup could not delete the webhook: %v", err)
		}
	})
}

// The live oracle of the diff refusal: the API answers 409 to an update that keeps the url and
// the event name of a webhook without an inbox. This goes red first if MailSlurp drops the check.
func TestTheAPIRefusesAnAccountWebhookUpdateThatKeepsTheURLAndTheEvent(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	client, err := NewClient(defaultBaseURL, key)
	require.NoError(t, err)
	ctx := context.Background()

	name := newTestName(testWebhookKind)
	renamed := newTestName(testWebhookKind)
	options := func(url, webhookName string) WebhookOptions {
		return WebhookOptions{
			URL:       url,
			EventName: string(WebhookEventNewContact),
			Name:      webhookName,
			IncludeHeaders: &WebhookHeaders{
				Headers: []WebhookHeader{{Name: "x-pulumi-test-signature", Value: "first-value"}},
			},
		}
	}

	accountURL := lifecycleWebhookHost + name + "-account"
	account, err := client.CreateAccountWebhook(ctx, options(accountURL, name))
	require.NoError(t, err)
	require.NotEmpty(t, account.ID)
	deleteWebhookAfterTheTest(t, client, account.ID)

	_, err = client.UpdateWebhook(ctx, account.ID, nil, options(accountURL, renamed))
	require.Error(t, err, "the API must refuse an update that keeps the url and the event")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 409, apiErr.StatusCode)
	assert.Equal(t, "W_409_CONFLICT", apiErr.ErrorCode)

	// The same rename with a new url is accepted, which is why the diff refuses only the pair.
	_, err = client.UpdateWebhook(ctx, account.ID, nil,
		options(accountURL+"-updated", renamed))
	require.NoError(t, err)

	// The header endpoint carries no such check, and it is the path the provider takes.
	_, err = client.UpdateWebhookHeaders(ctx, account.ID, WebhookHeaders{
		Headers: []WebhookHeader{{Name: "x-pulumi-test-signature", Value: "second-value"}},
	})
	require.NoError(t, err)

	inboxURL := lifecycleWebhookHost + name + "-inbox"
	scoped, err := client.CreateInboxWebhook(ctx, sharedInboxID, options(inboxURL, name))
	require.NoError(t, err)
	require.NotEmpty(t, scoped.ID)
	deleteWebhookAfterTheTest(t, client, scoped.ID)

	_, err = client.UpdateWebhook(ctx, scoped.ID, nil, options(inboxURL, renamed))
	assert.NoError(t, err, "an inbox-scoped webhook accepts the update the account webhook refuses")
}

// headerValue reads one header out of the secret includeHeaders property.
func headerValue(t *testing.T, output property.Map, name string) string {
	t.Helper()
	headers := output.Get(webhookPropIncludeHeaders)
	require.True(t, headers.IsMap(), "the webhook reports no headers")
	got := headers.AsMap().Get(name)
	require.True(t, got.IsString(), "the webhook reports no %s header", name)
	return got.AsString()
}

// TestWebhookLifeCycle creates an account-wide webhook, so it creates no inbox and spends none of
// the inbox budget. It moves the webhook onto the shared inbox.
func TestWebhookLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	s := configuredServer(t, key)

	name := newTestName(testWebhookKind)
	renamed := newTestName(testWebhookKind)
	target := lifecycleWebhookHost + name
	movedTarget := target + "-updated"
	const headerName = "x-pulumi-test-signature"
	const firstValue = "first-value"
	const secondValue = "second-value"

	// Cleanup runs on failure too. It deletes by an identifier the list answered, never by a
	// value it did not capture, and it tolerates a webhook that is already gone.
	t.Cleanup(func() {
		ctx := context.WithoutCancel(context.Background())
		webhookSweeper.sweepMarked(ctx, key, name)
		webhookSweeper.sweepMarked(ctx, key, renamed)
	})

	type wanted struct {
		url         string
		name        string
		headerValue string
		inboxID     *string
	}
	inputs := func(w wanted) property.Map {
		m := map[string]property.Value{
			webhookPropURL:                 property.New(w.url),
			webhookPropName:                property.New(w.name),
			webhookPropEventName:           property.New(string(WebhookEventNewContact)),
			webhookPropRequestBodyTemplate: property.New(`{"webhookId":"{{webhookId}}"}`),
			webhookPropTags:                property.New([]property.Value{property.New(testNamePrefix + "lifecycle")}),
			webhookPropIncludeHeaders: property.New(property.NewMap(map[string]property.Value{
				headerName: property.New(w.headerValue),
			})),
		}
		if w.inboxID != nil {
			m[webhookPropInboxID] = property.New(*w.inboxID)
		}
		return property.NewMap(m)
	}

	var createdAt string
	// A diff that reports no change makes the harness skip a whole update leg, and it says nothing
	// when it does (pulumi-go-provider integration/integration.go:434). Each leg records that it ran.
	ran := map[string]bool{}
	const (
		urlLeg    = "the url change"
		headerLeg = "the header change"
		moveLeg   = "the move onto an inbox"
		renameLeg = "the rename"
	)

	integration.LifeCycleTest{
		Resource: tokens.Type(webhookResourceType),
		Create: integration.Operation{
			Inputs: inputs(wanted{url: target, name: name, headerValue: firstValue}),
			Hook: func(_, output property.Map) {
				assert.Equal(t, target, output.Get(webhookPropURL).AsString())
				assert.Equal(t, name, output.Get(webhookPropName).AsString())
				assert.Equal(t, string(WebhookEventNewContact), output.Get(webhookPropEventName).AsString())
				// The create response carries the headers under the read name.
				assert.Equal(t, firstValue, headerValue(t, output, headerName))
				assert.True(t, output.Get(webhookPropInboxID).IsNull(), "the webhook is account-wide")
				assert.Equal(t, "POST", output.Get("method").AsString())
				createdAt = output.Get("createdAt").AsString()
				assert.NotEmpty(t, createdAt)
			},
		},
		Updates: []integration.Operation{
			{
				// The url changes. A body without the other properties destroys them, so
				// this asserts the whole body travelled.
				Inputs: inputs(wanted{url: movedTarget, name: name, headerValue: firstValue}),
				Hook: func(_, output property.Map) {
					ran[urlLeg] = true
					assert.Equal(t, movedTarget, output.Get(webhookPropURL).AsString())
					assert.Equal(t, name, output.Get(webhookPropName).AsString(),
						"an omitted name would be destroyed by the update")
					assert.Equal(t, string(WebhookEventNewContact), output.Get(webhookPropEventName).AsString(),
						"an omitted event name reverts the subscription to EMAIL_RECEIVED")
					assert.Equal(t, firstValue, headerValue(t, output, headerName))
					assert.Equal(t, createdAt, output.Get("createdAt").AsString(),
						"a url change must not replace the webhook")
				},
			},
			{
				// The headers alone change, so the update takes the headers endpoint. The full
				// body would answer 409 here: the url and the event name both stay.
				Inputs: inputs(wanted{url: movedTarget, name: name, headerValue: secondValue}),
				Hook: func(_, output property.Map) {
					ran[headerLeg] = true
					assert.Equal(t, secondValue, headerValue(t, output, headerName))
					assert.Equal(t, movedTarget, output.Get(webhookPropURL).AsString(),
						"the headers endpoint must disturb nothing else")
					assert.Equal(t, name, output.Get(webhookPropName).AsString())
					assert.Equal(t, createdAt, output.Get("createdAt").AsString(),
						"a header change must not replace the webhook")
				},
			},
			{
				// The move rides a query parameter on the same update call.
				Inputs: inputs(wanted{
					url: movedTarget, name: name, headerValue: secondValue, inboxID: &sharedInboxID,
				}),
				Hook: func(_, output property.Map) {
					ran[moveLeg] = true
					assert.Equal(t, sharedInboxID, output.Get(webhookPropInboxID).AsString())
					assert.Equal(t, secondValue, headerValue(t, output, headerName))
					assert.Equal(t, createdAt, output.Get("createdAt").AsString(),
						"a move must not replace the webhook")
				},
			},
			{
				// A plain rename, with the url and the event name unchanged. The API accepts it
				// because the webhook now has an inbox.
				Inputs: inputs(wanted{
					url: movedTarget, name: renamed, headerValue: secondValue, inboxID: &sharedInboxID,
				}),
				Hook: func(_, output property.Map) {
					ran[renameLeg] = true
					assert.Equal(t, renamed, output.Get(webhookPropName).AsString())
					assert.Equal(t, sharedInboxID, output.Get(webhookPropInboxID).AsString())
					assert.Equal(t, createdAt, output.Get("createdAt").AsString(),
						"a rename must not replace the webhook")
				},
			},
		},
	}.Run(t, s)

	for _, leg := range []string{urlLeg, headerLeg, moveLeg, renameLeg} {
		require.True(t, ran[leg], "the harness skipped %s, so nothing proved it", leg)
	}
}

// The suite must leave the account as it found it, and the lifecycle test deletes its webhook.
func TestTheAccountHoldsNoWebhookAfterTheLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	count, err := countWebhooks(context.Background(), key)
	require.NoError(t, err)
	assert.Zero(t, count, "the webhook tests left a webhook behind")
}
