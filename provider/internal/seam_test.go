package internal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// indexModulePrefix is the module the ModuleMap rewrites the Go package to. Every name the
// provider publishes carries it, so a resource in another module fails the tests below.
const indexModulePrefix = "mailslurp:index:"

// committedSchemaPath is the schema file the build commits and the release ships.
const committedSchemaPath = "../cmd/pulumi-resource-mailslurp/schema.json"

// v1ResourceTokens and v1FunctionTokens are the whole published surface.
func v1ResourceTokens() []string {
	return []string{
		templateResourceType, inboxResourceType, forwarderResourceType,
		rulesetResourceType, webhookResourceType,
	}
}

func v1FunctionTokens() []string { return []string{getDomainFunction, getInboxFunction} }

// tokenSets is the part of the schema the registration seam decides.
type tokenSets struct {
	Resources map[string]json.RawMessage `json:"resources"`
	Functions map[string]json.RawMessage `json:"functions"`
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func assertTheV1Surface(t *testing.T, where string, sets tokenSets) {
	t.Helper()
	assert.Equal(t, v1ResourceTokens(), sortedKeys(sets.Resources), "%s resources", where)
	assert.Equal(t, v1FunctionTokens(), sortedKeys(sets.Functions), "%s functions", where)
	for _, token := range append(sortedKeys(sets.Resources), sortedKeys(sets.Functions)...) {
		assert.True(t, strings.HasPrefix(token, indexModulePrefix),
			"%s: the name %q is outside the %q module", where, token, indexModulePrefix)
	}
}

func TestTheProviderRegistersFiveResourcesAndTwoFunctionsInOneModule(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)
	resp, err := s.GetSchema(p.GetSchemaRequest{Version: 0})
	require.NoError(t, err)

	var sets tokenSets
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &sets))
	assertTheV1Surface(t, "the built provider", sets)
}

// committedResourceTokens answers the resources the shipped schema publishes. The coverage test
// below reads it rather than the hand list, so a new resource fails there and not two steps later.
func committedResourceTokens(t *testing.T) []string {
	t.Helper()
	var sets tokenSets
	require.NoError(t, json.Unmarshal(readCommittedSchema(t), &sets))
	return sortedKeys(sets.Resources)
}

func readCommittedSchema(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(committedSchemaPath))
	require.NoError(t, err, "the build commits the schema, and the release ships this file")
	return raw
}

func TestTheCommittedSchemaCarriesTheSameV1Surface(t *testing.T) {
	t.Parallel()
	var sets tokenSets
	require.NoError(t, json.Unmarshal(readCommittedSchema(t), &sets))
	assertTheV1Surface(t, committedSchemaPath, sets)
}

// The committed file feeds the SDK generators, the prose check, and the registry. A stale copy
// ships the wrong surface, so it must agree with the provider on every user-facing string.
func TestTheCommittedSchemaMatchesTheBuiltProvider(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)
	resp, err := s.GetSchema(p.GetSchemaRequest{Version: 0})
	require.NoError(t, err)

	var built, committed map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &built))
	require.NoError(t, json.Unmarshal(readCommittedSchema(t), &committed))

	// The generate step drops the version, which the linker sets and the committed file omits.
	delete(built, "version")
	delete(committed, "version")
	assert.Equal(t, built, committed,
		"run make generate_schema and commit %s", committedSchemaPath)
}

// The registry reads these values from the committed file, not from the Go source.
func TestTheCommittedSchemaCarriesTheRegistryMetadata(t *testing.T) {
	t.Parallel()
	var doc schemaDoc
	require.NoError(t, json.Unmarshal(readCommittedSchema(t), &doc))

	assert.Equal(t, "mailslurp", doc.Name)
	assert.Equal(t, "MailSlurp", doc.DisplayName)
	assert.Equal(t, "jschady", doc.Publisher)
	assert.Equal(t, "jschady", doc.Namespace)
	assert.Equal(t, "Apache-2.0", doc.License)
	assert.Equal(t, "https://www.mailslurp.com", doc.Homepage)
	assert.Equal(t, "https://github.com/jschady/pulumi-mailslurp", doc.Repository)
	assert.Equal(t, "github://api.github.com/jschady/pulumi-mailslurp", doc.PluginDownloadURL)
	assert.Equal(t, []string{"pulumi", "mailslurp", "email", "category/utility", "kind/native"},
		doc.Keywords)
	assert.Equal(t, "https://raw.githubusercontent.com/jschady/pulumi-mailslurp/main/docs/logo.svg",
		doc.LogoURL)
	assert.NotEmpty(t, doc.Description)

	// The download URL must name the repository as well as the owner, or the plugin download
	// asks GitHub for a release of the owner rather than of this provider.
	assert.Contains(t, doc.PluginDownloadURL, "pulumi-mailslurp")
}

// deleteCase names one delete per resource. Every delete needs the configured client, so a
// resource that holds its own configuration answers the not-configured diagnostic instead.
type deleteCase struct {
	token string
	urn   presource.URN
	id    string
	state property.Map
	on    func(*MockClient)
}

func deleteCases() []deleteCase {
	return []deleteCase{
		{inboxResourceType, inboxURN(), testInboxID, priorState(nil), func(m *MockClient) {
			m.EXPECT().DeleteInbox(gomock.Any(), testInboxID).Return(nil)
		}},
		{webhookResourceType, webhookURN(), testWebhookID, priorWebhookState(nil), func(m *MockClient) {
			m.EXPECT().DeleteWebhook(gomock.Any(), testWebhookID).Return(nil)
		}},
		{rulesetResourceType, rulesetURN(), testRulesetID, priorRulesetState(nil), func(m *MockClient) {
			m.EXPECT().DeleteRuleset(gomock.Any(), testRulesetID).Return(nil)
		}},
		{templateResourceType, templateURN(), testTemplateID, priorTemplateState(nil), func(m *MockClient) {
			m.EXPECT().DeleteTemplate(gomock.Any(), testTemplateID).Return(nil)
		}},
		{forwarderResourceType, forwarderURN(), testForwarderID, priorForwarderState(nil), func(m *MockClient) {
			m.EXPECT().DeleteForwarder(gomock.Any(), testForwarderID).Return(nil)
		}},
	}
}

// One Configure call must reach all seven registrations. NewProvider shares one *Config by
// reference, so the client that Configure builds is the client every resource and function uses.
func TestOneConfigureReachesEveryResourceAndFunction(t *testing.T) {
	t.Parallel()
	mock := NewMockClient(gomock.NewController(t))
	for _, tc := range deleteCases() {
		tc.on(mock)
	}
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(&InboxDto{ID: testInboxID}, nil)
	mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).
		Return(&DomainDto{ID: testDomainID, Domain: "example.com"}, nil)

	built := 0
	s := newTestServer(t, func(context.Context, *Config) (Client, error) {
		built++
		return mock, nil
	})
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: configMap("k", defaultBaseURL)}))
	assert.Equal(t, 1, built, "one server session builds one client")

	for _, tc := range deleteCases() {
		require.NoError(t,
			s.Delete(p.DeleteRequest{ID: tc.id, Urn: tc.urn, Properties: tc.state}), tc.token)
	}

	_, err := s.Invoke(p.InvokeRequest{
		Token: tokens.Type(getInboxFunction),
		Args:  property.NewMap(map[string]property.Value{"inboxId": property.New(testInboxID)}),
	})
	require.NoError(t, err, getInboxFunction)

	_, err = s.Invoke(p.InvokeRequest{
		Token: tokens.Type(getDomainFunction),
		Args:  property.NewMap(map[string]property.Value{"domainId": property.New(testDomainID)}),
	})
	require.NoError(t, err, getDomainFunction)
}

// Without Configure the shared configuration holds no client, so every registration refuses the
// call with the same diagnostic. This is the negative half of the threading proof.
func TestWithoutConfigureEveryResourceRefusesTheCall(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)
	for _, tc := range deleteCases() {
		err := s.Delete(p.DeleteRequest{ID: tc.id, Urn: tc.urn, Properties: tc.state})
		require.Error(t, err, "%s reached the API without a configured client", tc.token)
		assert.Contains(t, err.Error(), notConfiguredMessage, "%s", tc.token)
	}
}

// previewCreate runs one create preview through the provider server, which is the path the engine
// takes. A preview reaches no API method, so the server needs no client.
func previewCreate(t *testing.T, urn presource.URN, inputs property.Map) property.Map {
	t.Helper()
	s := newTestServer(t, nil)
	resp, err := s.Create(p.CreateRequest{Urn: urn, Properties: inputs, DryRun: true})
	require.NoError(t, err)
	return resp.Properties
}

// previewCase names what a create preview must answer for one resource.
type previewCase struct {
	title string
	urn   presource.URN
	// inputs are the create inputs of the preview.
	inputs property.Map
	// mirrored outputs repeat an input, so the preview knows them.
	mirrored []string
	// assigned outputs come from MailSlurp, so the preview must mark them unknown.
	assigned []string
}

func previewCases() []previewCase {
	return []previewCase{
		{
			title:    "inbox",
			urn:      inboxURN(),
			inputs:   property.NewMap(map[string]property.Value{inboxPropName: property.New("probe")}),
			mirrored: []string{inboxPropName},
			assigned: []string{inboxPropEmailAddress, propCreatedAt, propReadOnly},
		},
		{
			// A pinned address is the program's own value, so the preview must state it.
			title: "inbox with a pinned address",
			urn:   inboxURN(),
			inputs: property.NewMap(map[string]property.Value{
				inboxPropEmailAddress: property.New(testAddress),
			}),
			mirrored: []string{inboxPropEmailAddress},
			assigned: []string{propCreatedAt, propReadOnly},
		},
		{
			title: "webhook",
			urn:   webhookURN(),
			inputs: property.NewMap(map[string]property.Value{
				webhookPropURL: property.New("https://example.com/hook"),
			}),
			mirrored: []string{webhookPropURL},
			assigned: []string{webhookPropEventName, "method"},
		},
		{
			title: "ruleset",
			urn:   rulesetURN(),
			inputs: property.NewMap(map[string]property.Value{
				rulesetPropInboxID: property.New(testInboxID),
				rulesetPropScope:   property.New(string(RulesetScopeReceivingEmails)),
				rulesetPropAction:  property.New(string(RulesetActionBlock)),
				rulesetPropTarget:  property.New("probe@example.com"),
			}),
			mirrored: []string{rulesetPropInboxID, rulesetPropTarget},
			assigned: []string{rulesetPropHandler, propCreatedAt},
		},
		{
			title: "template",
			urn:   templateURN(),
			inputs: property.NewMap(map[string]property.Value{
				templatePropName:    property.New(testTemplateName),
				templatePropContent: property.New(testTemplateContent),
			}),
			mirrored: []string{templatePropName, templatePropContent},
			assigned: []string{templatePropVariables, propCreatedAt},
		},
		{
			title: "forwarder",
			urn:   forwarderURN(),
			inputs: property.NewMap(map[string]property.Value{
				forwarderPropInboxID:             property.New(testInboxID),
				forwarderPropForwardToRecipients: strs("probe@example.com"),
				forwarderPropField:               property.New(string(ForwarderFieldSubject)),
				forwarderPropMatch:               property.New(testForwarderMatch),
			}),
			mirrored: []string{forwarderPropInboxID, forwarderPropMatch},
			assigned: []string{propCreatedAt},
		},
	}
}

// A create preview must never state a value that MailSlurp assigns. The framework marks an output
// unknown when it repeats no input, and only in a preview (pulumi-go-provider infer/resource.go:551).
func TestACreatePreviewMarksEveryServerAssignedOutputUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range previewCases() {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			out := previewCreate(t, tc.urn, tc.inputs)
			for _, name := range tc.assigned {
				v, ok := out.GetOk(name)
				require.True(t, ok, "the preview drops %s", name)
				assert.True(t, v.IsComputed(),
					"the preview states %s as %v, and MailSlurp assigns it", name, v)
			}
			for _, name := range tc.mirrored {
				v, ok := out.GetOk(name)
				require.True(t, ok, "the preview drops %s", name)
				assert.False(t, v.IsComputed(),
					"the preview hides %s, which repeats an input the program wrote", name)
			}
		})
	}
}

// urnFor builds the URN of one resource, which the server reads to route a request.
func urnFor(token string) presource.URN {
	return presource.NewURN("test", "provider", "", tokens.Type(token), "test")
}

// inboxInputs is the whole optional input set of the Inbox, which declares no required input.
func inboxInputs(over map[string]property.Value) property.Map {
	m := map[string]property.Value{inboxPropName: property.New("the seam inbox")}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// inputBuilders answers a full, valid input map per resource. A resource without an entry fails
// the coverage test below rather than skipping quietly.
func inputBuilders() map[string]func(map[string]property.Value) property.Map {
	return map[string]func(map[string]property.Value) property.Map{
		inboxResourceType:     inboxInputs,
		webhookResourceType:   webhookInputs,
		rulesetResourceType:   rulesetInputs,
		templateResourceType:  templateInputs,
		forwarderResourceType: forwarderInputs,
	}
}

// schemaSurface is the part of the committed schema the required-input rule reads.
type schemaSurface struct {
	Resources map[string]struct {
		RequiredInputs  []string `json:"requiredInputs"`
		InputProperties map[string]struct {
			Type string `json:"type"`
			Ref  string `json:"$ref"`
		} `json:"inputProperties"`
	} `json:"resources"`
	Types map[string]struct {
		Type string `json:"type"`
	} `json:"types"`
}

// requiredInputsOfType answers each resource's required inputs of one JSON type, read from the
// committed schema. An enum input is string valued, so its reference resolves to the same rule.
func requiredInputsOfType(t *testing.T, want string) map[string][]string {
	t.Helper()
	var doc schemaSurface
	require.NoError(t, json.Unmarshal(readCommittedSchema(t), &doc))

	out := map[string][]string{}
	for token, resource := range doc.Resources {
		for _, name := range resource.RequiredInputs {
			prop, ok := resource.InputProperties[name]
			require.True(t, ok, "%s requires %s and declares no such input", token, name)
			kind := prop.Type
			if prop.Ref != "" {
				named, ok := doc.Types[strings.TrimPrefix(prop.Ref, "#/types/")]
				require.True(t, ok, "%s: the input %s names an unknown type", token, name)
				kind = named.Type
			}
			if kind == want {
				out[token] = append(out[token], name)
			}
		}
		sort.Strings(out[token])
	}
	return out
}

// The two shapes a required input takes today: a value a program writes as a string, and a list.
func requiredStringInputs(t *testing.T) map[string][]string {
	t.Helper()
	return requiredInputsOfType(t, "string")
}

func requiredListInputs(t *testing.T) map[string][]string {
	t.Helper()
	return requiredInputsOfType(t, "array")
}

// blankValues are the shapes a program writes when it means nothing. A diff turns the empty string
// into a removal of a property the schema requires, so the refusal must cover it as well.
func blankValues() map[string]string {
	return map[string]string{"an empty string": "", "a space": " ", "a tab": "\t"}
}

// A required property that carries the empty string is a mistake, not a value. Without a refusal
// the diff plans a removal of a property the schema requires, and the API answers 400.
func TestCheckRefusesEveryEmptyRequiredStringInput(t *testing.T) {
	t.Parallel()
	required := requiredStringInputs(t)
	require.NotEmpty(t, required)

	for token, names := range required {
		build, ok := inputBuilders()[token]
		require.True(t, ok, "%s has no input builder, so the rule cannot be checked", token)
		for _, name := range names {
			for title, blank := range blankValues() {
				t.Run(token+"."+name+" holds "+title, func(t *testing.T) {
					t.Parallel()
					s := newTestServer(t, nil)
					resp, err := s.Check(p.CheckRequest{
						Urn:    urnFor(token),
						Inputs: build(one(name, property.New(blank))),
					})
					require.NoError(t, err)
					require.Len(t, resp.Failures, 1, "%s.%s: the empty value passed the check", token, name)
					assert.Equal(t, name, resp.Failures[0].Property)
					assert.Contains(t, resp.Failures[0].Reason, "`"+name+"`",
						"the refusal must name the property the program left empty")
				})
			}
		}
	}
}

// A required list that carries no element is a mistake, not a value: the API refuses it, and a
// program that writes one means to name something. The refusal says so before the API does.
func TestCheckRefusesEveryEmptyRequiredListInput(t *testing.T) {
	t.Parallel()
	required := requiredListInputs(t)
	require.NotEmpty(t, required, "no resource requires a list, so this rule pins nothing")

	for token, names := range required {
		build, ok := inputBuilders()[token]
		require.True(t, ok, "%s has no input builder, so the rule cannot be checked", token)
		for _, name := range names {
			t.Run(token+"."+name, func(t *testing.T) {
				t.Parallel()
				s := newTestServer(t, nil)
				resp, err := s.Check(p.CheckRequest{
					Urn:    urnFor(token),
					Inputs: build(one(name, strs())),
				})
				require.NoError(t, err)
				require.Len(t, resp.Failures, 1, "%s.%s: the empty list passed the check", token, name)
				assert.Equal(t, name, resp.Failures[0].Property)
				assert.Contains(t, resp.Failures[0].Reason, "`"+name+"`",
					"the refusal must name the property the program left empty")
			})
		}
	}
}

// A valid input map must pass unchanged, or the refusal above blocks every program.
func TestCheckAcceptsEveryValidInputMap(t *testing.T) {
	t.Parallel()
	for token, build := range inputBuilders() {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			s := newTestServer(t, nil)
			resp, err := s.Check(p.CheckRequest{Urn: urnFor(token), Inputs: build(nil)})
			require.NoError(t, err)
			assert.Empty(t, resp.Failures)
			assert.Equal(t, build(nil), resp.Inputs, "the check must not rewrite a valid input map")
		})
	}
}

// The Inbox declares no required input, so the rule above refuses nothing on it. A later required
// input must reach the refusal, and this pin fails until it does.
func TestTheInboxDeclaresNoRequiredInput(t *testing.T) {
	t.Parallel()
	assert.Empty(t, requiredStringInputs(t)[inboxResourceType])
	assert.Empty(t, requiredListInputs(t)[inboxResourceType])
}

// Every resource the schema publishes must carry an input builder, or the checks above skip it in
// silence and a resource with a new required input reaches a stack unproven.
func TestEveryPublishedResourceCarriesAnInputBuilder(t *testing.T) {
	t.Parallel()
	var built []string
	for token := range inputBuilders() {
		built = append(built, token)
	}
	assert.ElementsMatch(t, committedResourceTokens(t), built,
		"the check tests read the input builders, so they must cover the published surface")
}

// A program can compute a required property from another resource. That value is unknown during a
// preview, so the refusal above must let it through rather than call it empty.
func TestCheckAcceptsAnUnknownRequiredInput(t *testing.T) {
	t.Parallel()
	required := requiredStringInputs(t)
	require.NotEmpty(t, required)

	for token, names := range required {
		build := inputBuilders()[token]
		for _, name := range names {
			t.Run(token+"."+name, func(t *testing.T) {
				t.Parallel()
				s := newTestServer(t, nil)
				resp, err := s.Check(p.CheckRequest{
					Urn:    urnFor(token),
					Inputs: build(one(name, property.New(property.Computed))),
				})
				require.NoError(t, err)
				assert.Empty(t, resp.Failures, "%s.%s: an unknown value is not an empty one", token, name)
			})
		}
	}
}

// The refusal wraps the default check, so a value of the wrong type still fails the check.
func TestCheckStillReportsADecodeFailure(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)
	resp, err := s.Check(p.CheckRequest{
		Urn:    urnFor(webhookResourceType),
		Inputs: webhookInputs(one(webhookPropURL, property.New(float64(7)))),
	})
	require.NoError(t, err)
	require.Len(t, resp.Failures, 1)
	assert.Equal(t, webhookPropURL, resp.Failures[0].Property)
}

// A program that leaves a required property out fails the default check. The blank refusal must
// not add a second failure for the same property.
func TestCheckReportsAnOmittedRequiredInputOnce(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)
	resp, err := s.Check(p.CheckRequest{
		Urn:    urnFor(templateResourceType),
		Inputs: property.NewMap(map[string]property.Value{templatePropName: property.New("a name")}),
	})
	require.NoError(t, err)
	require.Len(t, resp.Failures, 1)
	assert.Equal(t, templatePropContent, resp.Failures[0].Property)
}

// The harness runs a replacement leg only when the diff plans one, and says nothing when it skips
// a leg (pulumi-go-provider integration/integration.go:434). The ruleset live guard reads this.
func TestTheLifeCycleHarnessRunsAReplacementLegOnlyWhenTheDiffPlansAChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		title   string
		update  property.Map
		wantRun bool
	}{
		{"the update repeats the create inputs", rulesetInputs(nil), false},
		{"the update changes the action",
			rulesetInputs(one(rulesetPropAction, property.New(string(RulesetActionAllow)))), true},
	} {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			mock := NewMockClient(gomock.NewController(t))
			creates, deletes := 1, 1
			if tc.wantRun {
				creates, deletes = 2, 2
			}
			mock.EXPECT().CreateRuleset(gomock.Any(), testInboxID, gomock.Any()).
				Return(sampleRulesetDto(), nil).Times(creates)
			mock.EXPECT().DeleteRuleset(gomock.Any(), testRulesetID).Return(nil).Times(deletes)

			var replaced bool
			integration.LifeCycleTest{
				Resource: tokens.Type(rulesetResourceType),
				Create:   integration.Operation{Inputs: rulesetInputs(nil)},
				Updates: []integration.Operation{{
					Inputs: tc.update,
					Hook:   func(_, _ property.Map) { replaced = true },
				}},
			}.Run(t, mockedServer(t, mock))

			assert.Equal(t, tc.wantRun, replaced,
				"the harness runs the replacement hook only when the diff plans a change")
		})
	}
}
