//go:build integration

package internal

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "github.com/pulumi/pulumi-go-provider"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// createdObject is one vendor object that the session built.
type createdObject struct {
	token string
	urn   presource.URN
	id    string
	state property.Map
}

// One Configure call, one client, every registration reachable in the same session. The session
// creates no inbox: MailSlurp bills each one and the suite already spends the whole budget.
func TestOneSessionHoldsOneOfEveryResource(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	guardTheInboxBudget(t)

	// One session for every leg below. A resource that held its own configuration would answer the
	// not-configured diagnostic here instead of reaching MailSlurp.
	s := configuredServer(t, key)

	// The Inbox reaches the same client. The suite creates no sixth inbox, so this leg reads the
	// shared one rather than building another.
	read, err := s.Read(p.ReadRequest{
		ID:         sharedInboxID,
		Urn:        inboxURN(),
		Properties: property.NewMap(map[string]property.Value{}),
		Inputs:     property.NewMap(map[string]property.Value{}),
	})
	require.NoError(t, err, "the Inbox registration must reach the configured client")
	assert.Equal(t, sharedInboxID, read.ID)
	assert.Contains(t, read.Properties.Get(inboxPropEmailAddress).AsString(), "@")

	webhookName := newTestName(testWebhookKind)
	rulesetTarget := rulesetTargetFor(newTestName(testRulesetKind))
	templateName := newTestName(testTemplateKind)
	forwarderRecipient := forwarderRecipientFor(newTestName(testForwarderKind))

	// Every sweep is registered before its create, so an object the vendor built and then reported
	// as a failure is still removed. Each sweep matches the run pattern and nothing wider.
	t.Cleanup(func() {
		ctx := context.WithoutCancel(context.Background())
		webhookSweeper.sweepMarked(ctx, key, webhookName)
		rulesetSweeper.sweepMarked(ctx, key, rulesetTarget)
		templateSweeper.sweepMarked(ctx, key, templateName)
		forwarderSweeper.sweepMarked(ctx, key, forwarderRecipient)
	})

	// session carries the cleanups, so a leg that runs as a subtest keeps its object until the end.
	session := t
	// create registers the delete of the identifier the create answered. A blank identifier reaches
	// the whole collection, so it never becomes a delete.
	create := func(t *testing.T, token string, urn presource.URN, inputs property.Map) (createdObject, error) {
		t.Helper()
		check, err := s.Check(p.CheckRequest{Urn: urn, Inputs: inputs})
		require.NoError(t, err, token)
		require.Empty(t, check.Failures, token)

		resp, err := s.Create(p.CreateRequest{Urn: urn, Properties: check.Inputs})
		if err != nil {
			return createdObject{}, err
		}
		require.NotEmpty(t, resp.ID, "the create of %s captured no identifier", token)

		object := createdObject{token: token, urn: urn, id: resp.ID, state: resp.Properties}
		session.Cleanup(func() {
			if err := s.Delete(p.DeleteRequest{
				ID: object.id, Urn: object.urn, Properties: object.state,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "the cleanup could not delete %s: %v\n", object.token, err)
			}
		})
		return object, nil
	}

	var built []createdObject

	webhook, err := create(t, webhookResourceType, webhookURN(), property.NewMap(map[string]property.Value{
		webhookPropURL:       property.New(lifecycleWebhookHost + webhookName),
		webhookPropName:      property.New(webhookName),
		webhookPropEventName: property.New(string(WebhookEventNewContact)),
		webhookPropInboxID:   property.New(sharedInboxID),
	}))
	require.NoError(t, err)
	assert.Equal(t, sharedInboxID, webhook.state.Get(webhookPropInboxID).AsString())
	built = append(built, webhook)

	ruleset, err := create(t, rulesetResourceType, rulesetURN(), property.NewMap(map[string]property.Value{
		rulesetPropInboxID: property.New(sharedInboxID),
		rulesetPropScope:   property.New(string(RulesetScopeReceivingEmails)),
		rulesetPropAction:  property.New(string(RulesetActionBlock)),
		rulesetPropTarget:  property.New(rulesetTarget),
	}))
	require.NoError(t, err)
	assert.Equal(t, sharedInboxID, ruleset.state.Get(rulesetPropInboxID).AsString())
	built = append(built, ruleset)

	template, err := create(t, templateResourceType, templateURN(), property.NewMap(map[string]property.Value{
		templatePropName:    property.New(templateName),
		templatePropContent: property.New(testTemplateContent),
	}))
	require.NoError(t, err)
	assert.Equal(t, templateName, template.state.Get(templatePropName).AsString())
	built = append(built, template)

	// The forwarder leg alone depends on the account plan. It skips on the one plan-refusal code
	// and runs on every other answer, so the four legs above still prove the seam.
	t.Run("the forwarder leg", func(t *testing.T) {
		forwarder, err := create(t, forwarderResourceType, forwarderURN(),
			property.NewMap(map[string]property.Value{
				forwarderPropInboxID:             property.New(sharedInboxID),
				forwarderPropForwardToRecipients: strs(forwarderRecipient),
				forwarderPropField:               property.New(string(ForwarderFieldSubject)),
				forwarderPropMatch:               property.New(testForwarderMatch),
			}))
		if reason := skipReasonForForwarderEntitlement(err); reason != "" {
			t.Skip(reason)
		}
		require.NoError(t, err)
		assert.Equal(t, sharedInboxID, forwarder.state.Get(forwarderPropInboxID).AsString())
		built = append(built, forwarder)
	})

	require.GreaterOrEqual(t, len(built), 3, "the session must hold the account-wide resources")

	// The session deletes everything it built, in the same session that created it.
	for _, object := range built {
		require.NotEmpty(t, object.id, "%s carries no identifier", object.token)
		require.NoError(t, s.Delete(p.DeleteRequest{
			ID: object.id, Urn: object.urn, Properties: object.state,
		}), object.token)
	}

	// The account must hold nothing of this run.
	count, err := countWebhooks(context.Background(), key)
	require.NoError(t, err)
	assert.Zero(t, count, "the session left a webhook behind")

	rulesetIDs, err := rulesetIDsTargeting(context.Background(), key, rulesetTarget)
	require.NoError(t, err)
	assert.Empty(t, rulesetIDs, "the session left a ruleset behind")

	templateIDs, err := templateIDsNamed(context.Background(), key, templateName)
	require.NoError(t, err)
	assert.Empty(t, templateIDs, "the session left a template behind")

	forwarderIDs, err := forwarderIDsSendingTo(context.Background(), key, forwarderRecipient)
	require.NoError(t, err)
	assert.Empty(t, forwarderIDs, "the session left a forwarder behind")
}
