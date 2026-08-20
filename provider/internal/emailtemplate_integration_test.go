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

// templateIDsNamed answers the identifiers of the templates that carry one name. The lifecycle test
// reads it to prove that the update kept the one template the create built.
func templateIDsNamed(ctx context.Context, key, name string) ([]string, error) {
	var ids []string
	for page := range sweepMaxPages {
		found, err := listTemplates(ctx, key, page)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return ids, nil
		}
		for _, summary := range found {
			if summary.Name == name && summary.ID != "" {
				ids = append(ids, summary.ID)
			}
		}
	}
	return ids, nil
}

// variableNamesFrom reads the parsed variable list of one lifecycle answer, and checks the type of
// each variable against the value the pinned spec declares.
func variableNamesFrom(t *testing.T, output property.Map) []string {
	t.Helper()
	list := output.Get(templatePropVariables).AsArray()

	names := make([]string, 0, list.Len())
	for i := range list.Len() {
		variable := list.Get(i).AsMap()
		names = append(names, variable.Get(templatePropName).AsString())
		assert.Equal(t, templateVariableTypeString,
			variable.Get(templatePropVariableType).AsString())
	}
	return names
}

// TestEmailTemplateLifeCycle creates zero inboxes and spends no quota: a template belongs to the
// account. The `content` change proves the in-place update path.
func TestEmailTemplateLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)

	s := configuredServer(t, key)
	name := newTestName(testTemplateKind)

	// Cleanup runs on failure too. It deletes by an identifier the list answered, never by a value
	// it did not capture, and it tolerates a template that is already gone.
	t.Cleanup(func() {
		templateSweeper.sweepMarked(context.WithoutCancel(context.Background()), key, name)
	})

	inputs := func(content string) property.Map {
		return property.NewMap(map[string]property.Value{
			templatePropName:    property.New(name),
			templatePropContent: property.New(content),
		})
	}

	var firstID, createdAt string
	var updated bool

	integration.LifeCycleTest{
		Resource: tokens.Type(templateResourceType),
		Create: integration.Operation{
			Inputs: inputs(testTemplateContent),
			Hook: func(_, output property.Map) {
				assert.Equal(t, name, output.Get(templatePropName).AsString())
				assert.Equal(t, testTemplateContent, output.Get(templatePropContent).AsString())

				// The create never sends a variable list, so the server parsed this one out of the
				// content it received.
				assert.Equal(t, []string{testFirstVariable}, variableNamesFrom(t, output))

				createdAt = output.Get(templatePropCreatedAt).AsString()
				_, err := time.Parse(time.RFC3339, createdAt)
				assert.NoError(t, err, "the creation time must parse as RFC 3339: %q", createdAt)

				ids, err := templateIDsNamed(context.Background(), key, name)
				require.NoError(t, err)
				require.Len(t, ids, 1, "the create must build exactly one template")
				firstID = ids[0]
			},
		},
		Updates: []integration.Operation{
			{
				// The content changes and grows a second variable. The API updates the template in
				// place, so the engine never builds a replacement.
				Inputs: inputs(testContentNext),
				Hook: func(_, output property.Map) {
					updated = true
					assert.Equal(t, name, output.Get(templatePropName).AsString())
					assert.Equal(t, testContentNext, output.Get(templatePropContent).AsString())
					assert.ElementsMatch(t,
						[]string{testFirstVariable, testSecondVariable}, variableNamesFrom(t, output),
						"the server parses the variables out of the content on every write")
					assert.Equal(t, createdAt, output.Get(templatePropCreatedAt).AsString(),
						"an in-place update never moves the creation time")

					ids, err := templateIDsNamed(context.Background(), key, name)
					require.NoError(t, err)
					require.NotEmpty(t, firstID, "the create captured no identifier")
					assert.Equal(t, []string{firstID}, ids,
						"the update must keep the template the create built, and build no second one")
				},
			},
		},
	}.Run(t, s)

	// A diff that reports no change makes the harness skip the whole update leg, and every
	// assertion above runs inside it (pulumi-go-provider integration/integration.go:434).
	require.True(t, updated, "the update leg never ran, so nothing proved the in-place update")

	// The lifecycle deletes the template it created, so the account holds none of this run.
	ids, err := templateIDsNamed(context.Background(), key, name)
	require.NoError(t, err)
	assert.Empty(t, ids, "the lifecycle left a template behind")
}

// The suite must leave the account as it found it.
func TestTheAccountHoldsNoTestTemplateAfterTheLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)

	found, err := listTemplates(context.Background(), key, 0)
	require.NoError(t, err)
	for _, summary := range found {
		assert.False(t, testTemplateNamePattern.MatchString(summary.Name),
			"the template tests left %s behind", summary.Name)
	}
}
