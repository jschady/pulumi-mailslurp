//go:build integration

package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// configuredServer builds a provider server that talks to the real API.
func configuredServer(t *testing.T, key string) integration.Server {
	t.Helper()
	s := newTestServer(t, countingClientF)
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: configMap(key, defaultBaseURL)}))
	return s
}

// The shared inbox is the one inbox the suite builds for reading. Every inbox-scoped test reuses
// it instead of creating one of its own, because MailSlurp bills each create.
func TestTheSharedInboxIsReadable(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)

	client, err := NewClient(defaultBaseURL, key)
	require.NoError(t, err)

	got, err := client.GetInbox(context.Background(), sharedInboxID)
	require.NoError(t, err)
	assert.Equal(t, sharedInboxID, got.ID)
	require.NotNil(t, got.Name)
	assert.Equal(t, theSharedInboxName(t), *got.Name)
	assert.NotEmpty(t, got.EmailAddress)
}

// The pre-test sweep recovers from a crashed run, so a second sweep finds nothing. Every inbox
// this run created is younger than sweepMinAge, so the age gate protects it.
func TestASecondSweepFindsNothingToClean(t *testing.T) {
	key := requireAPIKey(t)
	assert.Zero(t, inboxSweeper.sweepStale(context.Background(), key),
		"the pre-test sweep left something behind")
}

// TestInboxLifeCycle spends 2 of the 3 budgeted inboxes, and the shared one is the third. It
// replaces on `prefix` rather than `domainName`: this account holds no verified custom domain.
func TestInboxLifeCycle(t *testing.T) {
	key := requireAPIKey(t)
	s := configuredServer(t, key)

	createdName := newTestName(testInboxKind)
	renamedName := newTestName(testInboxKind)
	const description = "The lifecycle inbox of the pulumi-mailslurp integration suite."
	const addressPrefix = "pulumitest"

	// Cleanup runs on failure too, and tolerates an inbox that is already gone.
	t.Cleanup(func() {
		ctx := context.WithoutCancel(context.Background())
		inboxSweeper.sweepMarked(ctx, key, createdName)
		inboxSweeper.sweepMarked(ctx, key, renamedName)
	})

	inputs := func(name string, prefix *string) property.Map {
		m := map[string]property.Value{
			inboxPropName:        property.New(name),
			inboxPropDescription: property.New(description),
			inboxPropTags:        property.New([]property.Value{property.New(testNamePrefix + "lifecycle")}),
		}
		if prefix != nil {
			m[inboxPropPrefix] = property.New(*prefix)
		}
		return property.NewMap(m)
	}

	var firstAddress string
	// A diff that reports no change makes the harness skip a whole update leg, and it says nothing
	// when it does (pulumi-go-provider integration/integration.go:434). Each leg records that it ran.
	ran := map[string]bool{}
	const renameLeg, replaceLeg = "the rename", "the replacement"

	integration.LifeCycleTest{
		Resource: tokens.Type(inboxResourceType),
		Create: integration.Operation{
			Inputs: inputs(createdName, nil),
			Hook: func(_, output property.Map) {
				assert.Equal(t, createdName, output.Get(inboxPropName).AsString())
				assert.Equal(t, description, output.Get(inboxPropDescription).AsString())
				firstAddress = output.Get(inboxPropEmailAddress).AsString()
				assert.Contains(t, firstAddress, "@", "the API assigns an email address")
				assert.NotEmpty(t, output.Get("createdAt").AsString())
			},
		},
		Updates: []integration.Operation{
			{
				// An in-place rename. The inbox keeps its identity and its email address.
				Inputs: inputs(renamedName, nil),
				Hook: func(_, output property.Map) {
					ran[renameLeg] = true
					assert.Equal(t, renamedName, output.Get(inboxPropName).AsString())
					assert.Equal(t, firstAddress, output.Get(inboxPropEmailAddress).AsString(),
						"a rename must not replace the inbox")
				},
			},
			{
				// A replacement. The new inbox carries a new address built from the prefix.
				Inputs: inputs(renamedName, ptr(addressPrefix)),
				Hook: func(_, output property.Map) {
					ran[replaceLeg] = true
					address := output.Get(inboxPropEmailAddress).AsString()
					assert.NotEqual(t, firstAddress, address, "a prefix change must replace the inbox")
					assert.True(t, strings.HasPrefix(address, addressPrefix),
						"the replaced inbox must use the prefix: %s", address)
					assert.Equal(t, addressPrefix, output.Get(inboxPropPrefix).AsString(),
						"the write-only prefix is carried in the state")
				},
			},
		},
	}.Run(t, s)

	for _, leg := range []string{renameLeg, replaceLeg} {
		require.True(t, ran[leg], "the harness skipped %s, so nothing proved it", leg)
	}
}
