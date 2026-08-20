package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheWebhookDiffTableMatchesTheSchemaContract(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, prop := range webhookProperties() {
		require.False(t, seen[prop.name], "the table lists %s twice", prop.name)
		seen[prop.name] = true
	}
	for _, name := range webhookUpdatable {
		assert.True(t, seen[name], "input property %q has no diff rule", name)
	}
	for _, name := range webhookNeverDiffed {
		assert.False(t, seen[name], "input property %q must never diff", name)
	}

	schema := getSchema(t)
	webhook := schema.Resources[webhookResourceType]
	require.NotNil(t, webhook)
	assert.Len(t, webhook.InputProperties, len(webhookUpdatable)+len(webhookNeverDiffed))
	for name := range webhook.InputProperties {
		assert.Contains(t, append(webhookUpdatable, webhookNeverDiffed...), name,
			"input property %q is in no contract list", name)
	}
}

// The header values ride a shared password to the consumer's server, so they are secret.
func TestSecretsIsSecret(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	webhook := schema.Resources[webhookResourceType]
	require.NotNil(t, webhook)

	input := webhook.InputProperties[webhookPropIncludeHeaders]
	require.NotNil(t, input, "the webhook has no includeHeaders input")
	assert.True(t, input.Secret, "the includeHeaders input must be secret")

	output := webhook.Properties[webhookPropIncludeHeaders]
	require.NotNil(t, output, "the webhook has no includeHeaders output")
	assert.True(t, output.Secret, "the includeHeaders output must be secret")

	apiKey := schema.Config.Variables["apiKey"]
	require.NotNil(t, apiKey)
	assert.True(t, apiKey.Secret)
}

func TestWebhookForcesNoReplacement(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	webhook := schema.Resources[webhookResourceType]
	require.NotNil(t, webhook)
	require.NotEmpty(t, webhook.InputProperties)
	for name, prop := range webhook.InputProperties {
		assert.False(t, prop.ReplaceOnChanges, "property %q must update in place", name)
	}
}

// basicAuth stays out of v1: the API reports the object as a boolean, and the webhook
// list reports the user name and the password as plain text.
func TestWebhookDoesNotModelBasicAuth(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	webhook := schema.Resources[webhookResourceType]
	require.NotNil(t, webhook)
	assert.NotContains(t, webhook.InputProperties, "basicAuth")
	assert.NotContains(t, webhook.Properties, "basicAuth")
	assert.Contains(t, webhook.Description, "basicAuth",
		"the description must state why the property is absent")
}

// The write property is includeHeaders and the read property is requestHeaders, so the schema
// must carry the write name and never the read name.
func TestWebhookSchemaUsesTheWritePropertyName(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	webhook := schema.Resources[webhookResourceType]
	require.NotNil(t, webhook)
	assert.Contains(t, webhook.InputProperties, webhookPropIncludeHeaders)
	assert.NotContains(t, webhook.InputProperties, "requestHeaders")
	assert.NotContains(t, webhook.Properties, "requestHeaders")
}

// The values come from the pinned API spec at test time, so a vendor value the spec grows fails
// here instead of reaching a stack outside the enum.
func TestWebhookEventNameEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	want := specEnumValues(t, "CreateWebhookOptions", webhookPropEventName)
	// One Go type serves the input and the output, which holds while both spec schemas agree.
	assert.Equal(t, want, specEnumValues(t, "WebhookDto", webhookPropEventName))
	assertEnumValues(t, "mailslurp:index:WebhookEventName", want)
}
