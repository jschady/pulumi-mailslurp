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

// rulesetIDsTargeting answers the identifiers of the rulesets that carry one target. The lifecycle
// test reads it to prove that a replacement built a second ruleset and then removed the first.
func rulesetIDsTargeting(ctx context.Context, key, target string) ([]string, error) {
	var ids []string
	for page := range sweepMaxPages {
		found, err := listRulesets(ctx, key, page)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return ids, nil
		}
		for _, summary := range found {
			if summary.Target == target && summary.ID != "" {
				ids = append(ids, summary.ID)
			}
		}
	}
	return ids, nil
}

// TestInboxRulesetLifeCycle creates zero inboxes of its own: every ruleset lands on the shared
// inbox. The `action` change proves the whole-resource replace path.
func TestInboxRulesetLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	guardTheInboxBudget(t)

	s := configuredServer(t, key)
	target := rulesetTargetFor(newTestName(testRulesetKind))

	// Cleanup runs on failure too. It deletes by an identifier the list answered, never by a value
	// it did not capture, and it tolerates a ruleset that is already gone.
	t.Cleanup(func() {
		rulesetSweeper.sweepMarked(context.WithoutCancel(context.Background()), key, target)
	})

	inputs := func(action RulesetAction) property.Map {
		return property.NewMap(map[string]property.Value{
			rulesetPropInboxID: property.New(sharedInboxID),
			rulesetPropScope:   property.New(string(RulesetScopeReceivingEmails)),
			rulesetPropAction:  property.New(string(action)),
			rulesetPropTarget:  property.New(target),
		})
	}

	var firstID, createdAt string
	// The ruleset has no update, so the leg below is a replacement. A diff that reports no change
	// makes the harness skip it silently (pulumi-go-provider integration/integration.go:434).
	var replaced bool

	integration.LifeCycleTest{
		Resource: tokens.Type(rulesetResourceType),
		Create: integration.Operation{
			Inputs: inputs(RulesetActionBlock),
			Hook: func(_, output property.Map) {
				assert.Equal(t, sharedInboxID, output.Get(rulesetPropInboxID).AsString(),
					"the create must answer the inbox that the query parameter named")
				assert.Equal(t, string(RulesetScopeReceivingEmails), output.Get(rulesetPropScope).AsString())
				assert.Equal(t, string(RulesetActionBlock), output.Get(rulesetPropAction).AsString())
				assert.Equal(t, target, output.Get(rulesetPropTarget).AsString())
				assert.Equal(t, rulesetHandlerException, output.Get(rulesetPropHandler).AsString())
				assert.True(t, output.Get("phoneId").IsNull(), "an inbox ruleset names no phone number")

				createdAt = output.Get(rulesetPropCreatedAt).AsString()
				_, err := time.Parse(time.RFC3339, createdAt)
				assert.NoError(t, err, "the creation time must parse as RFC 3339: %q", createdAt)

				ids, err := rulesetIDsTargeting(context.Background(), key, target)
				require.NoError(t, err)
				require.Len(t, ids, 1, "the create must build exactly one ruleset")
				firstID = ids[0]
			},
		},
		Updates: []integration.Operation{
			{
				// The action changes. The API offers no update, so the engine replaces the ruleset:
				// it creates the new one and then deletes the one the create answered.
				Inputs: inputs(RulesetActionAllow),
				Hook: func(_, output property.Map) {
					replaced = true
					assert.Equal(t, string(RulesetActionAllow), output.Get(rulesetPropAction).AsString())
					assert.Equal(t, target, output.Get(rulesetPropTarget).AsString())
					assert.NotEqual(t, createdAt, output.Get(rulesetPropCreatedAt).AsString(),
						"an action change must replace the ruleset")

					ids, err := rulesetIDsTargeting(context.Background(), key, target)
					require.NoError(t, err)
					require.NotEmpty(t, firstID, "the create captured no identifier")
					var built []string
					for _, id := range ids {
						if id != firstID {
							built = append(built, id)
						}
					}
					assert.Len(t, built, 1, "the replacement must build one new ruleset: %v", ids)
				},
			},
		},
	}.Run(t, s)

	require.True(t, replaced, "the harness skipped the replacement, so nothing proved it")

	// The lifecycle deletes the ruleset it last created, so the account holds none of this run.
	ids, err := rulesetIDsTargeting(context.Background(), key, target)
	require.NoError(t, err)
	assert.Empty(t, ids, "the lifecycle left a ruleset behind, the first one included")
}

// The suite must leave the account as it found it, and the shared inbox goes with it in teardown.
func TestTheAccountHoldsNoTestRulesetAfterTheLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)

	found, err := listRulesets(context.Background(), key, 0)
	require.NoError(t, err)
	for _, summary := range found {
		assert.False(t, rulesetTargetPattern.MatchString(summary.Target),
			"the ruleset tests left %s behind", summary.Target)
	}
}
