package internal

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const (
	webhookResourceType = "mailslurp:index:Webhook"
	testWebhookID       = "webhook-1"
	testWebhookURL      = "https://webhook-1.example.com/hook"
	testWebhookURLNext  = "https://webhook-2.example.com/hook"
	testHeaderName      = "x-pulumi-probe"
	testHeaderNameNext  = "x-second"
	testHeaderValue     = "a value"
	testHeaderValueNext = "a new value"
	testWebhookName     = "pulumi-test-webhook-abcd1234"
)

// The Webhook contract: every input updates in place, and the tags never diff.
var (
	webhookUpdatable = []string{
		webhookPropURL, webhookPropName, webhookPropEventName, webhookPropIncludeHeaders,
		webhookPropRequestBodyTemplate, webhookPropInboxID,
		webhookPropUseStaticIPRange, webhookPropIgnoreInsecureSSLCertificates,
	}
	webhookNeverDiffed = []string{webhookPropTags}
)

func webhookURN() presource.URN {
	return presource.NewURN("test", "provider", "", tokens.Type(webhookResourceType), "test")
}

func headerMap(name, value string) property.Value {
	return property.New(property.NewMap(map[string]property.Value{name: property.New(value)}))
}

// priorWebhookState is the stored state the tests diff against. Each case overrides the one
// property it exercises.
func priorWebhookState(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		webhookPropURL:                           property.New(testWebhookURL),
		webhookPropEventName:                     property.New(string(defaultWebhookEventName)),
		webhookPropUseStaticIPRange:              property.New(false),
		webhookPropIgnoreInsecureSSLCertificates: property.New(false),
		"method":                                 property.New("POST"),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// webhookInputs always carries the required url, which is the only required input.
func webhookInputs(over map[string]property.Value) property.Map {
	m := map[string]property.Value{webhookPropURL: property.New(testWebhookURL)}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// diffWebhook runs a diff through the provider server, which is the path that proves the
// framework routes to the resource's own Diff.
func diffWebhook(t *testing.T, state, inputs property.Map) (p.DiffResponse, error) {
	t.Helper()
	s := newTestServer(t, nil)
	return s.Diff(p.DiffRequest{
		ID:     testWebhookID,
		Urn:    webhookURN(),
		State:  state,
		Inputs: inputs,
	})
}

// inboxScoped puts the webhook on an inbox. That is the shape the API accepts every
// single-property update on: an account webhook refuses one, which the conflict test pins.
func inboxScoped(over map[string]property.Value) map[string]property.Value {
	m := map[string]property.Value{webhookPropInboxID: property.New(testInboxID)}
	for k, v := range over {
		m[k] = v
	}
	return m
}

// Every input property with a prior value and a changed value.
func webhookCases() []propertyCase {
	return []propertyCase{
		{webhookPropURL, property.New(testWebhookURL), property.New(testWebhookURLNext)},
		{webhookPropName, property.New("old name"), property.New("new name")},
		{webhookPropEventName, property.New(string(defaultWebhookEventName)),
			property.New(string(WebhookEventNewContact))},
		{webhookPropIncludeHeaders, headerMap(testHeaderName, "old"), headerMap(testHeaderName, "new")},
		{webhookPropRequestBodyTemplate, property.New(`{"a":1}`), property.New(`{"b":2}`)},
		{webhookPropInboxID, property.New("inbox-old"), property.New("inbox-new")},
		{webhookPropUseStaticIPRange, property.New(false), property.New(true)},
		{webhookPropIgnoreInsecureSSLCertificates, property.New(false), property.New(true)},
	}
}

func assertNeverReplaces(t *testing.T, resp p.DiffResponse) {
	t.Helper()
	for name, got := range resp.DetailedDiff {
		assert.NotEqual(t, p.AddReplace, got.Kind, "property %q", name)
		assert.NotEqual(t, p.UpdateReplace, got.Kind, "property %q", name)
		assert.NotEqual(t, p.DeleteReplace, got.Kind, "property %q", name)
	}
}

// Nothing on a webhook forces a replacement. The instant the resource implements
// CustomDiff, infer stops reading the replaceOnChanges tags, so the Diff owns every kind.
func TestWebhookDiffUpdatesInPlaceOnEveryInputProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range webhookCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffWebhook(t,
				priorWebhookState(inboxScoped(one(tc.property, tc.prior))),
				webhookInputs(inboxScoped(one(tc.property, tc.changed))))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a changed %s must produce a diff", tc.property)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.Update, got.Kind, "%s must update in place", tc.property)
			assertNeverReplaces(t, resp)
		})
	}
}

// The url is required, and the diff reads an unset event name and an unset flag as the value the
// server holds, so those four properties are never absent from the state.
var webhookAlwaysSet = []string{
	webhookPropURL, webhookPropEventName,
	webhookPropUseStaticIPRange, webhookPropIgnoreInsecureSSLCertificates,
}

func TestWebhookDiffAddsAPropertyThatAppears(t *testing.T) {
	t.Parallel()
	for _, tc := range webhookCases() {
		want := p.Add
		if slices.Contains(webhookAlwaysSet, tc.property) {
			want = p.Update
		}
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			state, inputs := inboxScoped(nil), inboxScoped(one(tc.property, tc.changed))
			if tc.property == webhookPropInboxID {
				// The move onto an inbox is how this property appears.
				state, inputs = nil, one(tc.property, tc.changed)
			}
			resp, err := diffWebhook(t, priorWebhookState(state), webhookInputs(inputs))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, want, got.Kind, "%s must change in place", tc.property)
			assertNeverReplaces(t, resp)
		})
	}
}

// The update replaces the whole options body, so an omitted property is destroyed. Clearing a
// property therefore converges, which is the opposite of the inbox.
func TestWebhookDiffClearsAPropertyInPlace(t *testing.T) {
	t.Parallel()
	for _, name := range []string{webhookPropName, webhookPropRequestBodyTemplate, webhookPropIncludeHeaders} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			prior := property.New(testHeaderValue)
			if name == webhookPropIncludeHeaders {
				prior = headerMap(testHeaderName, testHeaderValue)
			}
			resp, err := diffWebhook(t,
				priorWebhookState(inboxScoped(one(name, prior))),
				webhookInputs(inboxScoped(nil)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a removed %s must produce a diff", name)
			got, ok := resp.DetailedDiff[name]
			require.True(t, ok, "no detailed diff for %s", name)
			assert.Equal(t, p.Delete, got.Kind)
			assertNeverReplaces(t, resp)
		})
	}
}

// The move is a query parameter on the update, and it cannot name the account. A blank value
// asks the server to move the webhook to an unnamed inbox, which the client refuses.
func TestWebhookDiffRefusesToRemoveTheWebhookFromItsInbox(t *testing.T) {
	t.Parallel()
	_, err := diffWebhook(t,
		priorWebhookState(one(webhookPropInboxID, property.New("inbox-old"))),
		webhookInputs(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), webhookPropInboxID)
	for _, sentence := range splitSentences(err.Error()) {
		words := strings.Fields(backticked.ReplaceAllString(sentence, "X"))
		assert.LessOrEqual(t, len(words), 20, "sentence of %d words: %s", len(words), sentence)
	}
	assert.NotContains(t, strings.ToLower(err.Error()), "please")
}

// MailSlurp accepts the tags and never reports them, so a comparison would loop forever.
func TestWebhookDiffNeverDiffsTheTags(t *testing.T) {
	t.Parallel()
	resp, err := diffWebhook(t, priorWebhookState(nil),
		webhookInputs(one(webhookPropTags, strs("a", "b"))))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges, "the tags are write-only, so they must never diff")
	assert.Empty(t, resp.DetailedDiff)
}

// Both properties are server-computed outputs. A flapping health status must never plan an update.
func TestWebhookDiffNeverDiffsTheServerComputedOutputs(t *testing.T) {
	t.Parallel()
	state := priorWebhookState(map[string]property.Value{
		"healthStatus": property.New("UNHEALTHY"),
		"enabled":      property.New(true),
	})
	resp, err := diffWebhook(t, state, webhookInputs(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
}

func TestWebhookDiffReportsNoChangesWhenTheProgramMatchesTheState(t *testing.T) {
	t.Parallel()
	inputs := map[string]property.Value{}
	state := map[string]property.Value{}
	for _, tc := range webhookCases() {
		inputs[tc.property] = tc.prior
		state[tc.property] = tc.prior
	}
	resp, err := diffWebhook(t, priorWebhookState(state), webhookInputs(inputs))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
	assert.Empty(t, resp.DetailedDiff)
}

// An omitted eventName does not clear on the server: it resets the subscription to the default.
// The diff reads an unset input as that default, so a program without an eventName converges.
func TestWebhookDiffTreatsAnUnsetEventNameAsTheDefault(t *testing.T) {
	t.Parallel()
	resp, err := diffWebhook(t, priorWebhookState(nil), webhookInputs(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)

	resp, err = diffWebhook(t,
		priorWebhookState(one(webhookPropEventName, property.New(string(WebhookEventNewContact)))),
		webhookInputs(nil))
	require.NoError(t, err)
	assert.True(t, resp.HasChanges, "an unset eventName reverts the subscription to the default")
	require.Contains(t, resp.DetailedDiff, webhookPropEventName)
	assert.Equal(t, p.Update, resp.DetailedDiff[webhookPropEventName].Kind)
}

// The API answers null for both flags, so an unset input and a stored false mean the same thing.
func TestWebhookDiffTreatsAnUnsetFlagAsFalse(t *testing.T) {
	t.Parallel()
	for _, name := range []string{webhookPropUseStaticIPRange, webhookPropIgnoreInsecureSSLCertificates} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resp, err := diffWebhook(t,
				priorWebhookState(inboxScoped(one(name, property.New(false)))),
				webhookInputs(inboxScoped(nil)))
			require.NoError(t, err)
			assert.False(t, resp.HasChanges)

			resp, err = diffWebhook(t,
				priorWebhookState(inboxScoped(one(name, property.New(true)))),
				webhookInputs(inboxScoped(nil)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges)
			require.Contains(t, resp.DetailedDiff, name)
			assert.Equal(t, p.Update, resp.DetailedDiff[name].Kind)
		})
	}
}

// The header values are secret, so the engine hands them to the diff wrapped. A diff that cannot
// read through the wrapper would plan an update on every run.
func TestWebhookDiffReadsSecretHeaderValues(t *testing.T) {
	t.Parallel()
	same := headerMap(testHeaderName, testHeaderValue).WithSecret(true)
	resp, err := diffWebhook(t,
		priorWebhookState(one(webhookPropIncludeHeaders, headerMap(testHeaderName, testHeaderValue))),
		webhookInputs(one(webhookPropIncludeHeaders, same)))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges, "a secret value that matches the state must not diff")

	changed := headerMap(testHeaderName, testHeaderValueNext).WithSecret(true)
	resp, err = diffWebhook(t,
		priorWebhookState(one(webhookPropIncludeHeaders, headerMap(testHeaderName, testHeaderValue))),
		webhookInputs(one(webhookPropIncludeHeaders, changed)))
	require.NoError(t, err)
	assert.True(t, resp.HasChanges)
	require.Contains(t, resp.DetailedDiff, webhookPropIncludeHeaders)
	assert.Equal(t, p.Update, resp.DetailedDiff[webhookPropIncludeHeaders].Kind)
}

// A second header is a change even when the first one keeps its value.
func TestWebhookDiffReadsEveryHeader(t *testing.T) {
	t.Parallel()
	two := property.New(property.NewMap(map[string]property.Value{
		testHeaderName:     property.New(testHeaderValue),
		testHeaderNameNext: property.New("another value"),
	}))
	resp, err := diffWebhook(t,
		priorWebhookState(one(webhookPropIncludeHeaders, headerMap(testHeaderName, testHeaderValue))),
		webhookInputs(one(webhookPropIncludeHeaders, two)))
	require.NoError(t, err)
	assert.True(t, resp.HasChanges)
	require.Contains(t, resp.DetailedDiff, webhookPropIncludeHeaders)
}

// Go randomizes map iteration, so a render that does not sort makes one header map compare
// against itself as a change, and an account webhook then fails the diff with the conflict error.
func TestWebhookHeadersNormalizeInAStableOrder(t *testing.T) {
	t.Parallel()
	// The wanted body, in sorted order. The maps below carry the same four names.
	want := []WebhookHeader{
		{Name: "a-first", Value: "1"},
		{Name: "m-middle", Value: "2"},
		{Name: "q-then", Value: "3"},
		{Name: "z-last", Value: "4"},
	}
	values := map[string]string{}
	wrapped := map[string]property.Value{}
	for _, header := range want {
		values[header.Name] = header.Value
		wrapped[header.Name] = property.New(header.Value)
	}
	four := property.New(property.NewMap(wrapped))

	for range 24 {
		resp, err := diffWebhook(t,
			priorWebhookState(inboxScoped(one(webhookPropIncludeHeaders, four))),
			webhookInputs(inboxScoped(one(webhookPropIncludeHeaders, four))))
		require.NoError(t, err)
		require.False(t, resp.HasChanges, "an unchanged header map must never plan an update")
		require.Equal(t, want, headerList(values).Headers, "one map must always build one body")
	}
}

func TestWebhookDiffCallsNoAPIMethod(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test on any call.
	webhook, _ := webhookResource(t)
	resp, err := webhook.Diff(context.Background(), infer.DiffRequest[WebhookArgs, WebhookState]{
		ID:     testWebhookID,
		State:  WebhookState{URL: testWebhookURL, EventName: defaultWebhookEventName},
		Inputs: WebhookArgs{URL: testWebhookURLNext},
	})
	require.NoError(t, err)
	assert.True(t, resp.HasChanges)
}

// Verified in the framework source: infer replaces on every change until the resource
// implements CustomUpdate, and it drops the replaceOnChanges tags once it implements CustomDiff.
func TestWebhookImplementsTheFrameworkInterfacesTheContractNeeds(t *testing.T) {
	t.Parallel()
	var resource any = &Webhook{}
	_, hasUpdate := resource.(infer.CustomUpdate[WebhookArgs, WebhookState])
	assert.True(t, hasUpdate, "without CustomUpdate the framework replaces the webhook on every change")
	_, hasDiff := resource.(infer.CustomDiff[WebhookArgs, WebhookState])
	assert.True(t, hasDiff, "the tags and the server-computed outputs need a diff of our own")
}

// The diff table is the sole source of the kinds, so it must cover every input the schema
// carries except the tags, which the API never reports.

func TestWebhookDiffRefusesAnUpdateTheAPIAnswersWithAConflict(t *testing.T) {
	t.Parallel()
	_, err := diffWebhook(t,
		priorWebhookState(one(webhookPropName, property.New("old name"))),
		webhookInputs(one(webhookPropName, property.New("new name"))))
	require.Error(t, err)
	assert.Contains(t, err.Error(), webhookPropURL)
	assert.Contains(t, err.Error(), webhookPropEventName)
	for _, sentence := range splitSentences(err.Error()) {
		words := strings.Fields(backticked.ReplaceAllString(sentence, "X"))
		assert.LessOrEqual(t, len(words), 20, "sentence of %d words: %s", len(words), sentence)
	}
}

// The conflict is account-wide only. Every other shape of the same change reaches the API.
func TestWebhookDiffAllowsTheUpdatesTheAPIAccepts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		state  map[string]property.Value
		inputs map[string]property.Value
	}{
		{"the url changes with the name",
			one(webhookPropName, property.New("old name")),
			map[string]property.Value{
				webhookPropName: property.New("new name"),
				webhookPropURL:  property.New(testWebhookURLNext),
			}},
		{"the event name changes with the name",
			one(webhookPropName, property.New("old name")),
			map[string]property.Value{
				webhookPropName:      property.New("new name"),
				webhookPropEventName: property.New(string(WebhookEventNewContact)),
			}},
		{"the webhook moves onto an inbox",
			one(webhookPropName, property.New("old name")),
			map[string]property.Value{
				webhookPropName:    property.New("new name"),
				webhookPropInboxID: property.New(testInboxID),
			}},
		{"the webhook already has an inbox",
			map[string]property.Value{
				webhookPropName:    property.New("old name"),
				webhookPropInboxID: property.New(testInboxID),
			},
			map[string]property.Value{
				webhookPropName:    property.New("new name"),
				webhookPropInboxID: property.New(testInboxID),
			}},
		{"the headers alone change",
			one(webhookPropIncludeHeaders, headerMap(testHeaderName, testHeaderValue)),
			one(webhookPropIncludeHeaders, headerMap(testHeaderName, testHeaderValueNext))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp, err := diffWebhook(t, priorWebhookState(tt.state), webhookInputs(tt.inputs))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges)
			assertNeverReplaces(t, resp)
		})
	}
}

// The headers endpoint writes the headers alone. It is the only update path an account
// webhook accepts when the url and the event name stay the same.
