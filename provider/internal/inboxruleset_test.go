package internal

import (
	"context"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const (
	rulesetResourceType = "mailslurp:index:InboxRuleset"
	testRulesetID       = "ruleset-1"
	testRulesetIDNext   = "ruleset-2"
	testInboxIDNext     = "inbox-2"
	testRulesetTarget   = "*@spam.example.com"
	testTargetNext      = "*@other.example.com"
	testRulesetTime     = "2026-08-18T15:24:48.111Z"
)

// updateMarker owns the only `Update` method the compile-time check below can reach.
type updateMarker struct{}

func (updateMarker) Update() {}

// rulesetWithoutUpdate pairs the resource with a marker that declares `Update` at the same depth.
// The API offers no update call, so infer's default diff replaces on every change.
type rulesetWithoutUpdate struct {
	InboxRuleset
	updateMarker
}

// The selector below is ambiguous, and this package stops compiling, the moment InboxRuleset
// declares an Update method with any signature and either receiver form.
var _ = (*rulesetWithoutUpdate).Update

// The InboxRuleset contract: all four inputs force a replacement, and nothing updates.
var rulesetReplaceForcing = []string{
	rulesetPropInboxID, rulesetPropScope, rulesetPropAction, rulesetPropTarget,
}

func rulesetURN() presource.URN {
	return presource.NewURN("test", "provider", "", tokens.Type(rulesetResourceType), "test")
}

// rulesetResource builds the resource with a mock client already in place, which is what Configure
// does in production.
func rulesetResource(t *testing.T) (*InboxRuleset, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &InboxRuleset{config: &Config{client: mock}}, mock
}

func sampleRulesetDto() *RulesetDto {
	return &RulesetDto{
		ID:        testRulesetID,
		InboxID:   ptr(testInboxID),
		Scope:     string(RulesetScopeReceivingEmails),
		Action:    string(RulesetActionBlock),
		Target:    testRulesetTarget,
		Handler:   rulesetHandlerException,
		CreatedAt: testRulesetTime,
	}
}

// priorRulesetState is the stored state the diff reads. Each case overrides the one property it
// exercises.
func priorRulesetState(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		rulesetPropInboxID:   property.New(testInboxID),
		rulesetPropScope:     property.New(string(RulesetScopeReceivingEmails)),
		rulesetPropAction:    property.New(string(RulesetActionBlock)),
		rulesetPropTarget:    property.New(testRulesetTarget),
		rulesetPropHandler:   property.New(rulesetHandlerException),
		rulesetPropCreatedAt: property.New(testRulesetTime),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// rulesetInputs always carries the four required inputs, because the API requires every one.
func rulesetInputs(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		rulesetPropInboxID: property.New(testInboxID),
		rulesetPropScope:   property.New(string(RulesetScopeReceivingEmails)),
		rulesetPropAction:  property.New(string(RulesetActionBlock)),
		rulesetPropTarget:  property.New(testRulesetTarget),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// diffRuleset runs a diff through the provider server, which is the path that proves the framework
// replaces on every change without a hand-written Diff.
func diffRuleset(t *testing.T, state, inputs property.Map) (p.DiffResponse, error) {
	t.Helper()
	s := newTestServer(t, nil)
	return s.Diff(p.DiffRequest{
		ID:     testRulesetID,
		Urn:    rulesetURN(),
		State:  state,
		Inputs: inputs,
	})
}

// Every input property, with a prior value and a changed value.
func rulesetCases() []propertyCase {
	return []propertyCase{
		{rulesetPropInboxID, property.New(testInboxID), property.New(testInboxIDNext)},
		{rulesetPropScope, property.New(string(RulesetScopeReceivingEmails)),
			property.New(string(RulesetScopeSendingEmails))},
		{rulesetPropAction, property.New(string(RulesetActionBlock)),
			property.New(string(RulesetActionAllow))},
		{rulesetPropTarget, property.New(testRulesetTarget), property.New(testTargetNext)},
	}
}

// A change to any of the four inputs replaces the ruleset, because the API offers no
// update call and the resource implements no Update method.
func TestInboxRulesetReplacesOnEveryInputProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range rulesetCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffRuleset(t,
				priorRulesetState(one(tc.property, tc.prior)),
				rulesetInputs(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a changed %s must produce a diff", tc.property)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.UpdateReplace, got.Kind, "%s must replace the ruleset", tc.property)
			assert.Len(t, resp.DetailedDiff, 1, "only %s changed", tc.property)
		})
	}
}

func TestInboxRulesetPlansNothingWhenTheInputsMatchTheState(t *testing.T) {
	t.Parallel()
	resp, err := diffRuleset(t, priorRulesetState(nil), rulesetInputs(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
	assert.Empty(t, resp.DetailedDiff)
}

// The resource implements no Update method. The `var _` selector above stops the build
// when one appears; this pins the interface the framework reads to route an update.
func TestTheInboxRulesetImplementsNoUpdate(t *testing.T) {
	t.Parallel()
	var resource any = &InboxRuleset{}
	_, isUpdatable := resource.(infer.CustomUpdate[InboxRulesetArgs, InboxRulesetState])
	assert.False(t, isUpdatable, "an InboxRuleset that can update never replaces on a change")

	_, hasMethod := reflect.TypeOf(resource).MethodByName("Update")
	assert.False(t, hasMethod, "the type carries an Update method of some other shape")
}

// The resource carries no hand-written diff: `replaceOnChanges` is obeyed by the default diff
// alone, and this resource needs no property excluded from the comparison.
func TestTheInboxRulesetImplementsNoCustomDiff(t *testing.T) {
	t.Parallel()
	var resource any = &InboxRuleset{}
	_, isCustom := resource.(infer.CustomDiff[InboxRulesetArgs, InboxRulesetState])
	assert.False(t, isCustom, "a custom diff stops infer from reading the replaceOnChanges tags")
}

func TestInboxRulesetCreatePostsTheRulesetForTheConfiguredInbox(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)

	var got RulesetOptions
	mock.EXPECT().CreateRuleset(gomock.Any(), testInboxID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts RulesetOptions) (*RulesetDto, error) {
			got = opts
			return sampleRulesetDto(), nil
		})

	resp, err := ruleset.Create(context.Background(), infer.CreateRequest[InboxRulesetArgs]{
		Inputs: InboxRulesetArgs{
			InboxID: testInboxID,
			Scope:   RulesetScopeReceivingEmails,
			Action:  RulesetActionBlock,
			Target:  testRulesetTarget,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, string(RulesetScopeReceivingEmails), got.Scope)
	assert.Equal(t, string(RulesetActionBlock), got.Action)
	assert.Equal(t, testRulesetTarget, got.Target)

	assert.Equal(t, testRulesetID, resp.ID)
	assert.Equal(t, testInboxID, resp.Output.InboxID)
	assert.Equal(t, RulesetScopeReceivingEmails, resp.Output.Scope)
	assert.Equal(t, RulesetActionBlock, resp.Output.Action)
	assert.Equal(t, testRulesetTarget, resp.Output.Target)
	assert.Equal(t, rulesetHandlerException, resp.Output.Handler)
	assert.Equal(t, testRulesetTime, resp.Output.CreatedAt)
	assert.Nil(t, resp.Output.PhoneID, "an inbox ruleset names no phone number")
}

// The create response can report no inbox, and the API marks the property nullable. The state
// keeps the inbox the program named, because a blank required input plans a replacement forever.
func TestInboxRulesetCreateKeepsTheNamedInboxWhenTheAPIReportsNone(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)

	dto := sampleRulesetDto()
	dto.InboxID = nil
	mock.EXPECT().CreateRuleset(gomock.Any(), testInboxID, gomock.Any()).Return(dto, nil)

	resp, err := ruleset.Create(context.Background(), infer.CreateRequest[InboxRulesetArgs]{
		Inputs: InboxRulesetArgs{
			InboxID: testInboxID,
			Scope:   RulesetScopeReceivingEmails,
			Action:  RulesetActionBlock,
			Target:  testRulesetTarget,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, testInboxID, resp.Output.InboxID)
}

func TestInboxRulesetCreateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test if the preview reaches the API.
	ruleset, _ := rulesetResource(t)

	resp, err := ruleset.Create(context.Background(), infer.CreateRequest[InboxRulesetArgs]{
		Inputs: InboxRulesetArgs{
			InboxID: testInboxID,
			Scope:   RulesetScopeSendingEmails,
			Action:  RulesetActionFilterRemove,
			Target:  testRulesetTarget,
		},
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "a preview must not claim an id")
	assert.Equal(t, testInboxID, resp.Output.InboxID)
	assert.Equal(t, RulesetScopeSendingEmails, resp.Output.Scope)
	assert.Equal(t, RulesetActionFilterRemove, resp.Output.Action)
	assert.Equal(t, testRulesetTarget, resp.Output.Target)
}

// A provider that never ran Configure carries no configuration, and a Configure that failed can
// leave a configuration without a client. The create must refuse both.
func TestInboxRulesetCreateFailsWhenTheProviderIsNotConfigured(t *testing.T) {
	t.Parallel()
	for title, ruleset := range map[string]*InboxRuleset{
		titleWithoutConfiguration: {},
		titleWithoutClient:        {config: &Config{}},
	} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			_, err := ruleset.Create(context.Background(), infer.CreateRequest[InboxRulesetArgs]{})
			requireNotConfigured(t, err)
		})
	}
}

func TestInboxRulesetCreateReportsTheAPIError(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().CreateRuleset(gomock.Any(), testInboxID, gomock.Any()).
		Return(nil, &APIError{StatusCode: http.StatusBadRequest, Message: "the target is not valid"})

	_, err := ruleset.Create(context.Background(), infer.CreateRequest[InboxRulesetArgs]{
		Inputs: InboxRulesetArgs{InboxID: testInboxID, Target: testRulesetTarget},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the target is not valid")
}

// Read serves a refresh and an import, so it answers the inputs as well as the state.
func TestInboxRulesetReadAnswersTheStateAndTheInputs(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().GetRuleset(gomock.Any(), testRulesetID).Return(sampleRulesetDto(), nil)

	resp, err := ruleset.Read(context.Background(),
		infer.ReadRequest[InboxRulesetArgs, InboxRulesetState]{ID: testRulesetID})
	require.NoError(t, err)

	assert.Equal(t, testRulesetID, resp.ID)
	assert.Equal(t, testInboxID, resp.State.InboxID)
	assert.Equal(t, RulesetActionBlock, resp.State.Action)
	assert.Equal(t, rulesetHandlerException, resp.State.Handler)

	// An import starts with no input at all, so the inputs must come from the API answer.
	assert.Equal(t, testInboxID, resp.Inputs.InboxID)
	assert.Equal(t, RulesetScopeReceivingEmails, resp.Inputs.Scope)
	assert.Equal(t, RulesetActionBlock, resp.Inputs.Action)
	assert.Equal(t, testRulesetTarget, resp.Inputs.Target)
}

// The API marks `inboxId` nullable, so a refresh keeps the prior inbox when the answer omits it.
func TestInboxRulesetReadKeepsThePriorInboxWhenTheAPIReportsNone(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	dto := sampleRulesetDto()
	dto.InboxID = nil
	mock.EXPECT().GetRuleset(gomock.Any(), testRulesetID).Return(dto, nil)

	resp, err := ruleset.Read(context.Background(),
		infer.ReadRequest[InboxRulesetArgs, InboxRulesetState]{
			ID:    testRulesetID,
			State: InboxRulesetState{InboxID: testInboxID},
		})
	require.NoError(t, err)
	assert.Equal(t, testInboxID, resp.State.InboxID)
	assert.Equal(t, testInboxID, resp.Inputs.InboxID)
}

// A deleted ruleset is drift. An empty response tells the engine the ruleset is gone.
func TestInboxRulesetReadReportsARulesetThatIsGone(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().GetRuleset(gomock.Any(), testRulesetID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	resp, err := ruleset.Read(context.Background(),
		infer.ReadRequest[InboxRulesetArgs, InboxRulesetState]{ID: testRulesetID})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "an empty response reports the ruleset as gone")
}

// A 404 that carries any other code is a real error, because an unrouted path answers
// 404 as well, and dropping the ruleset from the state would lose it.
func TestInboxRulesetReadReportsAnyOther404AsAnError(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().GetRuleset(gomock.Any(), testRulesetID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  "W_404_SOMETHING_ELSE",
		Message:    "the path is not routed",
	})

	_, err := ruleset.Read(context.Background(),
		infer.ReadRequest[InboxRulesetArgs, InboxRulesetState]{ID: testRulesetID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the path is not routed")
}

func TestInboxRulesetDeleteRemovesTheRuleset(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().DeleteRuleset(gomock.Any(), testRulesetID).Return(nil)

	_, err := ruleset.Delete(context.Background(),
		infer.DeleteRequest[InboxRulesetState]{ID: testRulesetID})
	require.NoError(t, err)
}

func TestInboxRulesetDeleteToleratesARulesetThatIsAlreadyGone(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().DeleteRuleset(gomock.Any(), testRulesetID).Return(&APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	_, err := ruleset.Delete(context.Background(),
		infer.DeleteRequest[InboxRulesetState]{ID: testRulesetID})
	require.NoError(t, err)
}

func TestInboxRulesetDeleteReportsAnyOtherError(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().DeleteRuleset(gomock.Any(), testRulesetIDNext).Return(&APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "the server failed",
	})

	_, err := ruleset.Delete(context.Background(),
		infer.DeleteRequest[InboxRulesetState]{ID: testRulesetIDNext})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the server failed")
}

// `PATCH /rulesets` and `PUT /rulesets` are test evaluators. The mock refuses every call
// the test does not expect, so a lifecycle method that reached another endpoint fails here.
func TestEveryInboxRulesetLifecycleMethodCallsOneRulesetEndpointOnly(t *testing.T) {
	t.Parallel()
	ruleset, mock := rulesetResource(t)
	mock.EXPECT().CreateRuleset(gomock.Any(), testInboxID, gomock.Any()).Return(sampleRulesetDto(), nil)
	mock.EXPECT().GetRuleset(gomock.Any(), testRulesetID).Return(sampleRulesetDto(), nil)
	mock.EXPECT().DeleteRuleset(gomock.Any(), testRulesetID).Return(nil)

	ctx := context.Background()
	_, err := ruleset.Create(ctx, infer.CreateRequest[InboxRulesetArgs]{
		Inputs: InboxRulesetArgs{
			InboxID: testInboxID,
			Scope:   RulesetScopeReceivingEmails,
			Action:  RulesetActionBlock,
			Target:  testRulesetTarget,
		},
	})
	require.NoError(t, err)
	_, err = ruleset.Read(ctx, infer.ReadRequest[InboxRulesetArgs, InboxRulesetState]{ID: testRulesetID})
	require.NoError(t, err)
	_, err = ruleset.Delete(ctx, infer.DeleteRequest[InboxRulesetState]{ID: testRulesetID})
	require.NoError(t, err)
}

// A source scan catches a wiring that no behavioural assertion can reach: an endpoint the resource
// must never name. It reads the literal spellings as well as the http package constants.
func TestTheInboxRulesetSourceNeverNamesATestEndpoint(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("inboxruleset.go")
	require.NoError(t, err)
	source := string(raw)

	for _, banned := range []string{
		"MethodPatch", "MethodPut", "PATCH", "PUT", "testNewRuleset", "testInboxRulesetsForInbox",
		"test-sending", "test-receiving",
	} {
		assert.NotContains(t, source, banned,
			"the resource must never name %s: the ruleset test endpoints are evaluators", banned)
	}
}

// The scan above reads spellings, so a request built from parts evades it. The import set is
// structure: with no transport package in reach, no path can call an evaluator endpoint at all.
func TestTheInboxRulesetSourceReachesTheAPIThroughTheClientAlone(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "inboxruleset.go", nil, parser.ImportsOnly)
	require.NoError(t, err)

	var got []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		got = append(got, path)
	}

	assert.ElementsMatch(t, []string{"context", "errors", embedImportPath, inferImportPath}, got,
		"the resource speaks to MailSlurp through the Client interface alone")
}

func rulesetSchema(t *testing.T) *schemaResource {
	t.Helper()
	schema := getSchema(t)
	ruleset := schema.Resources[rulesetResourceType]
	require.NotNil(t, ruleset, "the schema exposes no %s", rulesetResourceType)
	return ruleset
}

// `inboxId`, `scope`, `action` and `target` all carry `replaceOnChanges`, and the
// resource takes no other input.
func TestInboxRulesetTagsEveryInputAsReplaceOnChanges(t *testing.T) {
	t.Parallel()
	ruleset := rulesetSchema(t)
	require.Len(t, ruleset.InputProperties, len(rulesetReplaceForcing))

	for _, name := range rulesetReplaceForcing {
		prop := ruleset.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.True(t, prop.ReplaceOnChanges, "property %q must force a replacement", name)
	}
	assert.ElementsMatch(t, rulesetReplaceForcing, ruleset.RequiredInputs,
		"the API requires every one of the four inputs")
}

func TestEveryInboxRulesetInputCarriesTheReplaceWarning(t *testing.T) {
	t.Parallel()
	ruleset := rulesetSchema(t)
	for _, name := range rulesetReplaceForcing {
		prop := ruleset.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.Contains(t, prop.Description, "Warning:", "property %q", name)
		assert.Contains(t, prop.Description, "replace", "property %q", name)
	}
	assert.Contains(t, ruleset.Description, "replace",
		"the resource description must state that any change replaces the ruleset")
}

func TestInboxRulesetStateCoversEveryInput(t *testing.T) {
	t.Parallel()
	ruleset := rulesetSchema(t)
	require.NotEmpty(t, ruleset.InputProperties)
	for name := range ruleset.InputProperties {
		assert.Contains(t, ruleset.Properties, name,
			"input property %q has no output of the same name, so the diff cannot read it from the state", name)
	}
}

// The handler carries one value, and a new vendor value must reach a stack rather than break it,
// so the schema keeps the property a plain string and the description names the value.
func TestTheInboxRulesetHandlerIsAPlainStringThatNamesItsOnlyValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{rulesetHandlerException}, specEnumValues(t, "RulesetDto", rulesetPropHandler))

	schema := getSchema(t)
	assert.NotContains(t, schema.Types, "mailslurp:index:RulesetHandler")

	handler := schema.Resources[rulesetResourceType].Properties[rulesetPropHandler]
	require.NotNil(t, handler)
	assert.Contains(t, handler.Description, rulesetHandlerException)
}

func TestRulesetScopeEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	want := specEnumValues(t, "CreateRulesetOptions", rulesetPropScope)
	// One Go type serves the input and the output, which holds while both spec schemas agree.
	assert.Equal(t, want, specEnumValues(t, "RulesetDto", rulesetPropScope))
	assertEnumValues(t, "mailslurp:index:RulesetScope", want)
}

func TestRulesetActionEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	want := specEnumValues(t, "CreateRulesetOptions", rulesetPropAction)
	assert.Equal(t, want, specEnumValues(t, "RulesetDto", rulesetPropAction))
	assertEnumValues(t, "mailslurp:index:RulesetAction", want)
}

// Ruleset naming and sweep policy. The API gives a ruleset no name, so the target
// carries the run marker. The integration sweeper reads these, and this file carries no build tag.
const testRulesetKind = "ruleset"

// rulesetTargetFor builds the match target of one run from a newTestName value. The host belongs to
// the reserved documentation domain, so no real address ever matches it.
func rulesetTargetFor(name string) string { return "*@" + name + ".example.com" }

// rulesetTargetPattern matches only what rulesetTargetFor builds, so a target this package did not
// build is never a sweep candidate.
var rulesetTargetPattern = regexp.MustCompile(
	`^\*@` + testNamePrefix + testRulesetKind + `-[0-9a-f]{8}\.example\.com$`)

// isSweepableTestRuleset reports whether the sweeper may delete a ruleset. A blank identifier would
// reach the whole collection, and a young ruleset can belong to a concurrent run.
func isSweepableTestRuleset(id, target string, createdAt, now time.Time) bool {
	if id == "" || !rulesetTargetPattern.MatchString(target) {
		return false
	}
	return now.Sub(createdAt) > sweepMinAge
}

// The integration sweeper deletes rulesets from a live account, so its predicate answers false for
// every target this package did not build, and for an identifier it did not capture.
func TestTheSweeperMatchesOnlyTheRulesetTargetsTheTestsBuild(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	now := old.Add(24 * time.Hour)

	for _, target := range []string{
		rulesetTargetFor(newTestName(testRulesetKind)),
		"*@pulumi-test-ruleset-00000000.example.com",
		"*@pulumi-test-ruleset-ffffffff.example.com",
	} {
		assert.True(t, isSweepableTestRuleset(testRulesetID, target, old, now), "must sweep %q", target)
	}

	for _, target := range []string{
		"", testRulesetTarget, "user@example.com", wildcardTarget,
		"*@pulumi-test-inbox-abcd1234.example.com", "*@pulumi-test-ruleset-ABCD1234.example.com",
		"*@pulumi-test-ruleset-abcd123.example.com", "*@pulumi-test-ruleset-abcd12345.example.com",
		"*@pulumi-test-ruleset-abcd1234.example.org", "*@pulumi-test-ruleset-abcd1234.example.com.test",
		" *@pulumi-test-ruleset-abcd1234.example.com", "*@pulumi-test-ruleset-abcd1234.example.com\n",
	} {
		assert.False(t, isSweepableTestRuleset(testRulesetID, target, old, now), "must not sweep %q", target)
	}

	sweepable := rulesetTargetFor(newTestName(testRulesetKind))
	assert.False(t, isSweepableTestRuleset("", sweepable, old, now),
		"a blank identifier reaches the whole collection, so it is never a sweep candidate")
	assert.False(t, isSweepableTestRuleset(testRulesetID, sweepable, now.Add(-time.Minute), now),
		"a fresh ruleset can belong to a concurrent run")
}
