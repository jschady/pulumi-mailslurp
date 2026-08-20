package internal

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi-go-provider/integration"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const (
	forwarderResourceType   = "mailslurp:index:InboxForwarder"
	forwarderMatchGroupType = "mailslurp:index:InboxForwarderMatchGroup"
	forwarderMatchRuleType  = "mailslurp:index:InboxForwarderMatchRule"

	testForwarderID     = "forwarder-1"
	testForwarderIDNext = "forwarder-2"
	testForwarderName   = "the name the server wrote"
	testForwarderTime   = "2026-08-18T15:24:48.111Z"
	testForwardTo       = "forward@example.com"
	testForwardToNext   = "audit@example.com"
	testForwarderMatch  = "weekly*"
	testMatchNext       = "monthly*"
	testRuleValue       = "reports@example.com"

	// The members of the nested match tree. The resource never spells them, so they live here.
	forwarderPropOperator = "operator"
	forwarderPropMatches  = "matches"
	forwarderPropGroups   = "groups"
	forwarderPropValue    = "value"
)

func forwarderURN() presource.URN {
	return presource.NewURN("test", "provider", "", tokens.Type(forwarderResourceType), "test")
}

// forwarderResource builds the resource with a mock client already in place, which is what Configure
// does in production.
func forwarderResource(t *testing.T) (*InboxForwarder, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &InboxForwarder{config: &Config{client: mock}}, mock
}

func sampleForwarderDto() *ForwarderDto {
	return &ForwarderDto{
		ID:                  testForwarderID,
		InboxID:             ptr(testInboxID),
		Name:                ptr(testForwarderName),
		Field:               ptr(string(ForwarderFieldSubject)),
		Should:              ptr(string(ForwarderShouldWildcard)),
		Match:               ptr(testForwarderMatch),
		ForwardToRecipients: []string{testForwardTo},
		MatchOptions: &ForwarderMatchOptions{
			Operator: string(ForwarderMatchOperatorAnd),
			Matches: []ForwarderMatch{{
				Field:  string(ForwarderFieldSender),
				Should: string(ForwarderShouldContain),
				Value:  testRuleValue,
			}},
		},
		CreatedAt: testForwarderTime,
	}
}

func sampleForwarderArgs() InboxForwarderArgs {
	return InboxForwarderArgs{
		InboxID:             testInboxID,
		ForwardToRecipients: []string{testForwardTo},
		Field:               ptr(ForwarderFieldSubject),
		Should:              ptr(ForwarderShouldWildcard),
		Match:               ptr(testForwarderMatch),
		MatchOptions: &InboxForwarderMatchGroup{
			Operator: ForwarderMatchOperatorAnd,
			Matches: []InboxForwarderMatchRule{{
				Field:  ForwarderFieldSender,
				Should: ForwarderShouldContain,
				Value:  testRuleValue,
			}},
		},
	}
}

// matchGroupValue renders one match tree the way the state and the inputs carry it. It names no
// nested group, which is the shape a program writes when one group is enough.
func matchGroupValue(operator ForwarderMatchOperator, value string) property.Value {
	return property.New(map[string]property.Value{
		forwarderPropOperator: property.New(string(operator)),
		forwarderPropMatches: property.New([]property.Value{
			property.New(map[string]property.Value{
				forwarderPropField:  property.New(string(ForwarderFieldSender)),
				forwarderPropShould: property.New(string(ForwarderShouldContain)),
				forwarderPropValue:  property.New(value),
			}),
		}),
	})
}

// priorForwarderState is the stored state the diff reads. Each case overrides the one property it
// exercises.
func priorForwarderState(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		forwarderPropInboxID:             property.New(testInboxID),
		forwarderPropForwardToRecipients: strs(testForwardTo),
		forwarderPropField:               property.New(string(ForwarderFieldSubject)),
		forwarderPropShould:              property.New(string(ForwarderShouldWildcard)),
		forwarderPropMatch:               property.New(testForwarderMatch),
		forwarderPropMatchOptions:        matchGroupValue(ForwarderMatchOperatorAnd, testRuleValue),
		forwarderPropName:                property.New(testForwarderName),
		forwarderPropCreatedAt:           property.New(testForwarderTime),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// forwarderInputs carries every input, because the resource always sends the whole options body.
func forwarderInputs(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		forwarderPropInboxID:             property.New(testInboxID),
		forwarderPropForwardToRecipients: strs(testForwardTo),
		forwarderPropField:               property.New(string(ForwarderFieldSubject)),
		forwarderPropShould:              property.New(string(ForwarderShouldWildcard)),
		forwarderPropMatch:               property.New(testForwarderMatch),
		forwarderPropMatchOptions:        matchGroupValue(ForwarderMatchOperatorAnd, testRuleValue),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// diffForwarder runs a diff through the provider server, which is the path that proves the framework
// plans the update kinds this resource wants.
func diffForwarder(t *testing.T, state, inputs property.Map) (p.DiffResponse, error) {
	t.Helper()
	s := newTestServer(t, nil)
	return s.Diff(p.DiffRequest{
		ID:     testForwarderID,
		Urn:    forwarderURN(),
		State:  state,
		Inputs: inputs,
	})
}

// forwarderDiffKinds answers the kinds the diff reported under one property. A nested change lands
// on a path such as `matchOptions.operator` or `forwardToRecipients[0]`, not on the property alone.
func forwarderDiffKinds(resp p.DiffResponse, property string) []p.DiffKind {
	var kinds []p.DiffKind
	for name, diff := range resp.DetailedDiff {
		if name == property ||
			strings.HasPrefix(name, property+".") || strings.HasPrefix(name, property+"[") {
			kinds = append(kinds, diff.Kind)
		}
	}
	return kinds
}

// The five options of the update body, each with a prior value and a changed value.
func forwarderUpdatableCases() []propertyCase {
	return []propertyCase{
		{forwarderPropForwardToRecipients, strs(testForwardTo), strs(testForwardToNext)},
		{forwarderPropField,
			property.New(string(ForwarderFieldSubject)), property.New(string(ForwarderFieldSender))},
		{forwarderPropShould,
			property.New(string(ForwarderShouldWildcard)), property.New(string(ForwarderShouldEqual))},
		{forwarderPropMatch, property.New(testForwarderMatch), property.New(testMatchNext)},
		{forwarderPropMatchOptions,
			matchGroupValue(ForwarderMatchOperatorAnd, testRuleValue),
			matchGroupValue(ForwarderMatchOperatorOr, testRuleValue)},
	}
}

// The inbox rides the create call as a query parameter, so a move replaces the forwarder.
func forwarderReplaceCases() []propertyCase {
	return []propertyCase{
		{forwarderPropInboxID, property.New(testInboxID), property.New(testInboxIDNext)},
	}
}

// A change to any of the five options updates the forwarder in place, because the API takes the
// whole options body on a PUT to the forwarder itself.
func TestInboxForwarderUpdatesEveryOptionInPlace(t *testing.T) {
	t.Parallel()
	for _, tc := range forwarderUpdatableCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffForwarder(t,
				priorForwarderState(one(tc.property, tc.prior)),
				forwarderInputs(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a changed %s must produce a diff", tc.property)

			kinds := forwarderDiffKinds(resp, tc.property)
			require.NotEmpty(t, kinds, "no detailed diff for %s", tc.property)
			assert.Len(t, resp.DetailedDiff, len(kinds), "only %s changed", tc.property)
			for _, kind := range kinds {
				assert.Equal(t, p.Update, kind, "%s must update in place", tc.property)
			}
		})
	}
}

// A change to the inbox replaces the forwarder. The create call names the inbox in a query
// parameter, and no other call can move a forwarder to another inbox.
func TestInboxForwarderReplacesOnTheInbox(t *testing.T) {
	t.Parallel()
	for _, tc := range forwarderReplaceCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffForwarder(t,
				priorForwarderState(one(tc.property, tc.prior)),
				forwarderInputs(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a changed %s must produce a diff", tc.property)

			kinds := forwarderDiffKinds(resp, tc.property)
			require.Len(t, kinds, 1, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.UpdateReplace, kinds[0], "%s must replace the forwarder", tc.property)
			assert.Len(t, resp.DetailedDiff, 1, "only %s changed", tc.property)
		})
	}
}

// Every input must carry a diff case, so a property this file never diffs cannot reach a stack
// unproven.
func TestEveryInboxForwarderInputCarriesADiffCase(t *testing.T) {
	t.Parallel()
	var covered []string
	for _, tc := range append(forwarderUpdatableCases(), forwarderReplaceCases()...) {
		covered = append(covered, tc.property)
	}

	var inputs []string
	for name := range forwarderSchema(t).InputProperties {
		inputs = append(inputs, name)
	}
	require.ElementsMatch(t, inputs, covered, "every input property must carry a diff case")
}

func TestInboxForwarderPlansNothingWhenTheInputsMatchTheState(t *testing.T) {
	t.Parallel()
	resp, err := diffForwarder(t, priorForwarderState(nil), forwarderInputs(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
	assert.Empty(t, resp.DetailedDiff)
}

// Until a resource implements CustomUpdate, infer's default diff turns every change into a
// replacement. The five options update in place, so the Update must exist.
func TestTheInboxForwarderImplementsAnUpdate(t *testing.T) {
	t.Parallel()
	var resource any = &InboxForwarder{}
	_, isUpdatable := resource.(infer.CustomUpdate[InboxForwarderArgs, InboxForwarderState])
	assert.True(t, isUpdatable, "a forwarder without an Update replaces on every change")
}

// A hand-written diff stops infer from reading the `replaceOnChanges` tags, and the inbox needs one.
func TestTheInboxForwarderImplementsNoCustomDiff(t *testing.T) {
	t.Parallel()
	var resource any = &InboxForwarder{}
	_, isCustom := resource.(infer.CustomDiff[InboxForwarderArgs, InboxForwarderState])
	assert.False(t, isCustom, "a custom diff stops infer from reading the replaceOnChanges tags")
}

// The inbox is the one input that forces a replacement, and the five options must not.
func TestInboxForwarderTagsOnlyTheInboxAsReplaceOnChanges(t *testing.T) {
	t.Parallel()
	forwarder := forwarderSchema(t)
	require.Len(t, forwarder.InputProperties,
		len(forwarderUpdatableCases())+len(forwarderReplaceCases()))

	for name, prop := range forwarder.InputProperties {
		want := name == forwarderPropInboxID
		assert.Equal(t, want, prop.ReplaceOnChanges,
			"property %q carries the wrong replacement tag", name)
	}
	assert.ElementsMatch(t,
		[]string{forwarderPropInboxID, forwarderPropForwardToRecipients}, forwarder.RequiredInputs,
		"the API requires the inbox and the recipients, and nothing else")
}

func TestInboxForwarderCreatePostsTheOptionsForTheConfiguredInbox(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)

	var got ForwarderOptions
	mock.EXPECT().CreateForwarder(gomock.Any(), testInboxID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts ForwarderOptions) (*ForwarderDto, error) {
			got = opts
			return sampleForwarderDto(), nil
		})

	resp, err := forwarder.Create(context.Background(), infer.CreateRequest[InboxForwarderArgs]{
		Inputs: sampleForwarderArgs(),
	})
	require.NoError(t, err)

	require.NotNil(t, got.Field)
	assert.Equal(t, string(ForwarderFieldSubject), *got.Field)
	assert.Equal(t, []string{testForwardTo}, got.ForwardToRecipients)

	assert.Equal(t, testForwarderID, resp.ID)
	assert.Equal(t, testInboxID, resp.Output.InboxID)
	assert.Equal(t, testForwarderMatch, *resp.Output.Match)
	assert.Equal(t, testForwarderName, *resp.Output.Name)
	assert.Equal(t, testForwarderTime, resp.Output.CreatedAt)
	require.NotNil(t, resp.Output.MatchOptions)
	assert.Equal(t, ForwarderMatchOperatorAnd, resp.Output.MatchOptions.Operator)
	require.Len(t, resp.Output.MatchOptions.Matches, 1)
	assert.Equal(t, testRuleValue, resp.Output.MatchOptions.Matches[0].Value)
}

// The API marks `inboxId` nullable, so the state keeps the inbox the program named when the answer
// omits it. A blank required input would plan a replacement forever.
func TestInboxForwarderCreateKeepsTheNamedInboxWhenTheAPIReportsNone(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)

	dto := sampleForwarderDto()
	dto.InboxID = nil
	mock.EXPECT().CreateForwarder(gomock.Any(), testInboxID, gomock.Any()).Return(dto, nil)

	resp, err := forwarder.Create(context.Background(), infer.CreateRequest[InboxForwarderArgs]{
		Inputs: sampleForwarderArgs(),
	})
	require.NoError(t, err)
	assert.Equal(t, testInboxID, resp.Output.InboxID)
}

// An empty nested list and an absent one mean the same thing, so the state must answer one shape.
// A state that kept the empty list would plan a diff against an input that names no group.
func TestTheInboxForwarderStateReadsAnEmptyNestedListAsNoList(t *testing.T) {
	t.Parallel()
	dto := sampleForwarderDto()
	dto.MatchOptions.Groups = []ForwarderMatchOptions{}
	dto.MatchOptions.Matches = []ForwarderMatch{}

	state := forwarderStateFrom(dto, testInboxID)
	require.NotNil(t, state.MatchOptions)
	assert.Nil(t, state.MatchOptions.Groups, "an empty group list means no group")
	assert.Nil(t, state.MatchOptions.Matches, "an empty rule list means no rule")
}

// The API answers the empty string for an option that carries no value, so the state must read the
// empty string and the absent property as the same thing.
func TestTheInboxForwarderStateReadsAnEmptyOptionAsNoOption(t *testing.T) {
	t.Parallel()
	dto := sampleForwarderDto()
	dto.Field = ptr("")
	dto.Should = ptr("")
	dto.Match = ptr("")
	dto.Name = ptr("")

	state := forwarderStateFrom(dto, testInboxID)
	assert.Nil(t, state.Field)
	assert.Nil(t, state.Should)
	assert.Nil(t, state.Match)
	assert.Nil(t, state.Name)
}

// The match tree nests, so the request body and the state must both carry a group inside a group. A
// mapping that dropped the nested groups would pass every assertion that reads one level.
func TestTheInboxForwarderCarriesANestedMatchGroupBothWays(t *testing.T) {
	t.Parallel()
	inputs := sampleForwarderArgs()
	inputs.MatchOptions.Groups = []InboxForwarderMatchGroup{{
		Operator: ForwarderMatchOperatorOr,
		Matches: []InboxForwarderMatchRule{{
			Field:  ForwarderFieldSubject,
			Should: ForwarderShouldEqual,
			Value:  testForwarderMatch,
		}},
		Groups: []InboxForwarderMatchGroup{{
			Operator: ForwarderMatchOperatorAnd,
			Matches: []InboxForwarderMatchRule{{
				Field:  ForwarderFieldRecipients,
				Should: ForwarderShouldContain,
				Value:  testForwardToNext,
			}},
		}},
	}}

	body := forwarderOptions(inputs).MatchOptions
	require.NotNil(t, body)
	require.Len(t, body.Groups, 1)
	assert.Equal(t, string(ForwarderMatchOperatorOr), body.Groups[0].Operator)
	require.Len(t, body.Groups[0].Groups, 1, "the body must carry the group inside the group")
	require.Len(t, body.Groups[0].Groups[0].Matches, 1)
	assert.Equal(t, testForwardToNext, body.Groups[0].Groups[0].Matches[0].Value)

	dto := sampleForwarderDto()
	dto.MatchOptions = body
	state := forwarderStateFrom(dto, testInboxID).MatchOptions
	require.NotNil(t, state)
	require.Len(t, state.Groups, 1)
	require.Len(t, state.Groups[0].Groups, 1, "the state must carry the group inside the group")
	assert.Equal(t, ForwarderFieldRecipients, state.Groups[0].Groups[0].Matches[0].Field)
	assert.Equal(t, inputs.MatchOptions, state, "the round trip must not change the tree")
}

// A program can name an empty value for an optional enum, and the API refuses the empty string, so
// the body must omit the option rather than send it.
func TestTheInboxForwarderBodyOmitsAnEmptyOption(t *testing.T) {
	t.Parallel()
	inputs := sampleForwarderArgs()
	inputs.Field = ptr(ForwarderField(""))
	inputs.Should = ptr(ForwarderShould(""))
	inputs.Match = ptr("")

	body := forwarderOptions(inputs)
	assert.Nil(t, body.Field, "an empty comparison target must not reach the body")
	assert.Nil(t, body.Should, "an empty comparison mode must not reach the body")
	assert.Nil(t, body.Match, "an empty match must not reach the body")
}

// The recipients are a required property, so a missing list must answer an empty one and never a
// hole in the state.
func TestTheInboxForwarderStateAnswersAnEmptyRecipientList(t *testing.T) {
	t.Parallel()
	dto := sampleForwarderDto()
	dto.ForwardToRecipients = nil

	state := forwarderStateFrom(dto, testInboxID)
	assert.NotNil(t, state.ForwardToRecipients)
	assert.Empty(t, state.ForwardToRecipients)
}

// The recipients are a required property, so a preview that names no address must plan an empty list
// and never a hole in the state the engine stores.
func TestTheInboxForwarderPreviewAnswersAnEmptyRecipientList(t *testing.T) {
	t.Parallel()
	forwarder, _ := forwarderResource(t)

	inputs := sampleForwarderArgs()
	inputs.ForwardToRecipients = nil

	resp, err := forwarder.Create(context.Background(), infer.CreateRequest[InboxForwarderArgs]{
		Inputs: inputs,
		DryRun: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp.Output.ForwardToRecipients)
	assert.Empty(t, resp.Output.ForwardToRecipients)
}

func TestInboxForwarderCreateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test if the preview reaches the API.
	forwarder, _ := forwarderResource(t)

	inputs := sampleForwarderArgs()
	inputs.Match = ptr(testMatchNext)

	resp, err := forwarder.Create(context.Background(), infer.CreateRequest[InboxForwarderArgs]{
		Inputs: inputs,
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "a preview must not claim an id")
	assert.Equal(t, testInboxID, resp.Output.InboxID)
	assert.Equal(t, testMatchNext, *resp.Output.Match)
	assert.Nil(t, resp.Output.Name, "only the server writes the name")
	assert.Equal(t, []string{testForwardTo}, resp.Output.ForwardToRecipients)
	assert.Equal(t, inputs.MatchOptions, resp.Output.MatchOptions,
		"the preview must carry the match tree the program named")
}

// A provider that never ran Configure carries no configuration, and a Configure that failed can
// leave a configuration without a client. Every lifecycle method must refuse both.
func TestEveryInboxForwarderLifecycleMethodRefusesAnUnconfiguredProvider(t *testing.T) {
	t.Parallel()
	for title, forwarder := range map[string]*InboxForwarder{
		titleWithoutConfiguration: {},
		titleWithoutClient:        {config: &Config{}},
	} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			_, err := forwarder.Create(ctx, infer.CreateRequest[InboxForwarderArgs]{})
			requireNotConfigured(t, err)

			_, err = forwarder.Read(ctx,
				infer.ReadRequest[InboxForwarderArgs, InboxForwarderState]{ID: testForwarderID})
			requireNotConfigured(t, err)

			_, err = forwarder.Update(ctx,
				infer.UpdateRequest[InboxForwarderArgs, InboxForwarderState]{ID: testForwarderID})
			requireNotConfigured(t, err)

			_, err = forwarder.Delete(ctx,
				infer.DeleteRequest[InboxForwarderState]{ID: testForwarderID})
			requireNotConfigured(t, err)
		})
	}
}

func TestInboxForwarderCreateReportsTheAPIError(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().CreateForwarder(gomock.Any(), testInboxID, gomock.Any()).
		Return(nil, &APIError{StatusCode: http.StatusBadRequest, Message: "the match is not valid"})

	_, err := forwarder.Create(context.Background(), infer.CreateRequest[InboxForwarderArgs]{
		Inputs: sampleForwarderArgs(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the match is not valid")
}

// Read serves a refresh and an import, so it answers the inputs as well as the state.
func TestInboxForwarderReadAnswersTheStateAndTheInputs(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().GetForwarder(gomock.Any(), testForwarderID).Return(sampleForwarderDto(), nil)

	resp, err := forwarder.Read(context.Background(),
		infer.ReadRequest[InboxForwarderArgs, InboxForwarderState]{ID: testForwarderID})
	require.NoError(t, err)

	assert.Equal(t, testForwarderID, resp.ID)
	assert.Equal(t, testInboxID, resp.State.InboxID)
	assert.Equal(t, testForwarderName, *resp.State.Name)

	// An import starts with no input at all, so the inputs must come from the API answer.
	assert.Equal(t, testInboxID, resp.Inputs.InboxID)
	assert.Equal(t, ForwarderFieldSubject, *resp.Inputs.Field)
	assert.Equal(t, ForwarderShouldWildcard, *resp.Inputs.Should)
	assert.Equal(t, testForwarderMatch, *resp.Inputs.Match)
	assert.Equal(t, []string{testForwardTo}, resp.Inputs.ForwardToRecipients)
	require.NotNil(t, resp.Inputs.MatchOptions)
	assert.Equal(t, ForwarderMatchOperatorAnd, resp.Inputs.MatchOptions.Operator)
}

// The API marks `inboxId` nullable, so a refresh keeps the prior inbox when the answer omits it.
func TestInboxForwarderReadKeepsThePriorInboxWhenTheAPIReportsNone(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	dto := sampleForwarderDto()
	dto.InboxID = nil
	mock.EXPECT().GetForwarder(gomock.Any(), testForwarderID).Return(dto, nil)

	resp, err := forwarder.Read(context.Background(),
		infer.ReadRequest[InboxForwarderArgs, InboxForwarderState]{
			ID:    testForwarderID,
			State: InboxForwarderState{InboxID: testInboxID},
		})
	require.NoError(t, err)
	assert.Equal(t, testInboxID, resp.State.InboxID)
	assert.Equal(t, testInboxID, resp.Inputs.InboxID)
}

// A deleted forwarder is drift. An empty response tells the engine the forwarder is gone.
func TestInboxForwarderReadReportsAForwarderThatIsGone(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().GetForwarder(gomock.Any(), testForwarderID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	resp, err := forwarder.Read(context.Background(),
		infer.ReadRequest[InboxForwarderArgs, InboxForwarderState]{ID: testForwarderID})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "an empty response reports the forwarder as gone")
}

// A 404 that carries any other code is a real error, because an unrouted path answers 404 as well,
// and dropping the forwarder from the state would lose it.
func TestInboxForwarderReadReportsAnyOther404AsAnError(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().GetForwarder(gomock.Any(), testForwarderID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  "W_404_ANOTHER_CODE",
		Message:    "the forwarder path is not routed",
	})

	_, err := forwarder.Read(context.Background(),
		infer.ReadRequest[InboxForwarderArgs, InboxForwarderState]{ID: testForwarderID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the forwarder path is not routed")
}

// updatedForwarderOptions runs one update that changed the match alone, and answers the body the
// resource sent.
func updatedForwarderOptions(t *testing.T) ForwarderOptions {
	t.Helper()
	forwarder, mock := forwarderResource(t)

	answer := sampleForwarderDto()
	answer.Match = ptr(testMatchNext)

	var got ForwarderOptions
	mock.EXPECT().UpdateForwarder(gomock.Any(), testForwarderID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts ForwarderOptions) (*ForwarderDto, error) {
			got = opts
			return answer, nil
		})

	inputs := sampleForwarderArgs()
	inputs.Match = ptr(testMatchNext)

	resp, err := forwarder.Update(context.Background(),
		infer.UpdateRequest[InboxForwarderArgs, InboxForwarderState]{
			ID:     testForwarderID,
			State:  forwarderStateFrom(sampleForwarderDto(), testInboxID),
			Inputs: inputs,
		})
	require.NoError(t, err)
	assert.Equal(t, testMatchNext, *resp.Output.Match)
	return got
}

// The update body is the whole create body, so a call that changed one option must still carry
// every other one.
func TestInboxForwarderUpdateSendsEveryOptionWhenOnlyTheMatchChanged(t *testing.T) {
	t.Parallel()
	got := updatedForwarderOptions(t)

	require.NotNil(t, got.Field)
	assert.Equal(t, string(ForwarderFieldSubject), *got.Field)
	require.NotNil(t, got.Should)
	assert.Equal(t, string(ForwarderShouldWildcard), *got.Should)
	require.NotNil(t, got.Match)
	assert.Equal(t, testMatchNext, *got.Match)
	assert.Equal(t, []string{testForwardTo}, got.ForwardToRecipients)

	require.NotNil(t, got.MatchOptions)
	assert.Equal(t, string(ForwarderMatchOperatorAnd), got.MatchOptions.Operator)
	require.Len(t, got.MatchOptions.Matches, 1)
	assert.Equal(t, string(ForwarderFieldSender), got.MatchOptions.Matches[0].Field)
	assert.Equal(t, testRuleValue, got.MatchOptions.Matches[0].Value)
}

// A hand list of members stops covering the body the moment the API grows an option, so the walk
// reads the members of the body itself and fails on the first one the update left empty.
func TestTheInboxForwarderUpdateLeavesNoOptionOfTheBodyEmpty(t *testing.T) {
	t.Parallel()
	got := reflect.ValueOf(updatedForwarderOptions(t))

	require.Positive(t, got.NumField())
	for i := range got.NumField() {
		assert.False(t, got.Field(i).IsZero(),
			"the update body left %s empty, so the PUT would clear it", got.Type().Field(i).Name)
	}
}

func TestInboxForwarderUpdateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test if the preview reaches the API.
	forwarder, _ := forwarderResource(t)

	inputs := sampleForwarderArgs()
	inputs.Match = ptr(testMatchNext)
	// The operator differs from the stored one, so the assertion below reads the wanted tree and
	// not the tree the state already carried.
	inputs.MatchOptions.Operator = ForwarderMatchOperatorOr

	resp, err := forwarder.Update(context.Background(),
		infer.UpdateRequest[InboxForwarderArgs, InboxForwarderState]{
			ID:     testForwarderID,
			State:  forwarderStateFrom(sampleForwarderDto(), testInboxID),
			Inputs: inputs,
			DryRun: true,
		})
	require.NoError(t, err)
	assert.Equal(t, testMatchNext, *resp.Output.Match)
	assert.Equal(t, testForwarderName, *resp.Output.Name, "an update never rewrites the name")
	assert.Equal(t, testForwarderTime, resp.Output.CreatedAt,
		"an update never moves the creation time")
	assert.Equal(t, inputs.MatchOptions, resp.Output.MatchOptions,
		"the preview must carry the match tree the program named")
}

func TestInboxForwarderUpdateReportsTheAPIError(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().UpdateForwarder(gomock.Any(), testForwarderID, gomock.Any()).
		Return(nil, &APIError{StatusCode: http.StatusBadRequest, Message: "the recipient is not valid"})

	_, err := forwarder.Update(context.Background(),
		infer.UpdateRequest[InboxForwarderArgs, InboxForwarderState]{
			ID:     testForwarderID,
			Inputs: sampleForwarderArgs(),
		})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the recipient is not valid")
}

func TestInboxForwarderDeleteRemovesTheForwarder(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().DeleteForwarder(gomock.Any(), testForwarderID).Return(nil)

	_, err := forwarder.Delete(context.Background(),
		infer.DeleteRequest[InboxForwarderState]{ID: testForwarderID})
	require.NoError(t, err)
}

func TestInboxForwarderDeleteToleratesAForwarderThatIsAlreadyGone(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().DeleteForwarder(gomock.Any(), testForwarderID).Return(&APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	_, err := forwarder.Delete(context.Background(),
		infer.DeleteRequest[InboxForwarderState]{ID: testForwarderID})
	require.NoError(t, err)
}

func TestInboxForwarderDeleteReportsAnyOtherError(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().DeleteForwarder(gomock.Any(), testForwarderIDNext).Return(&APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "the delete failed on the server",
	})

	_, err := forwarder.Delete(context.Background(),
		infer.DeleteRequest[InboxForwarderState]{ID: testForwarderIDNext})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the delete failed on the server")
}

// The list, the test and the event endpoints of the API must never reach a lifecycle method. The
// mock refuses every call the test does not expect, so a stray endpoint fails here.
func TestEveryInboxForwarderLifecycleMethodCallsOneForwarderEndpointOnly(t *testing.T) {
	t.Parallel()
	forwarder, mock := forwarderResource(t)
	mock.EXPECT().CreateForwarder(gomock.Any(), testInboxID, gomock.Any()).
		Return(sampleForwarderDto(), nil)
	mock.EXPECT().GetForwarder(gomock.Any(), testForwarderID).Return(sampleForwarderDto(), nil)
	mock.EXPECT().UpdateForwarder(gomock.Any(), testForwarderID, gomock.Any()).
		Return(sampleForwarderDto(), nil)
	mock.EXPECT().DeleteForwarder(gomock.Any(), testForwarderID).Return(nil)

	ctx := context.Background()
	inputs := sampleForwarderArgs()

	_, err := forwarder.Create(ctx, infer.CreateRequest[InboxForwarderArgs]{Inputs: inputs})
	require.NoError(t, err)
	_, err = forwarder.Read(ctx,
		infer.ReadRequest[InboxForwarderArgs, InboxForwarderState]{ID: testForwarderID})
	require.NoError(t, err)
	_, err = forwarder.Update(ctx, infer.UpdateRequest[InboxForwarderArgs, InboxForwarderState]{
		ID: testForwarderID, Inputs: inputs,
	})
	require.NoError(t, err)
	_, err = forwarder.Delete(ctx, infer.DeleteRequest[InboxForwarderState]{ID: testForwarderID})
	require.NoError(t, err)
}

// A source scan reads spellings, so a request built from parts evades it. The import set is
// structure: with no transport package in reach, the resource speaks through the client alone.
func TestTheInboxForwarderSourceReachesTheAPIThroughTheClientAlone(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "inboxforwarder.go", nil, parser.ImportsOnly)
	require.NoError(t, err)

	var got []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		got = append(got, path)
	}

	assert.ElementsMatch(t,
		[]string{contextImportPath, errorsImportPath, embedImportPath, inferImportPath}, got,
		"the resource speaks to MailSlurp through the Client interface alone")
}

// mockedServer builds a configured provider server over one mock client, so a lifecycle harness can
// run without a credential and without the network.
func mockedServer(t *testing.T, mock *MockClient) integration.Server {
	t.Helper()
	s := newTestServer(t, func(context.Context, *Config) (Client, error) { return mock, nil })
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: configMap("unused", defaultBaseURL)}))
	return s
}

// The lifecycle harness runs an update leg only when the diff plans a change, and it says nothing
// when it skips one (pulumi-go-provider integration/integration.go:434). The live guard reads this.
func TestTheLifeCycleHarnessRunsAnUpdateLegOnlyWhenTheDiffPlansAChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		title   string
		update  property.Map
		wantRun bool
	}{
		{"the update repeats the create inputs", forwarderInputs(nil), false},
		{"the update changes the match",
			forwarderInputs(one(forwarderPropMatch, property.New(testMatchNext))), true},
	} {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			mock := NewMockClient(gomock.NewController(t))
			mock.EXPECT().CreateForwarder(gomock.Any(), testInboxID, gomock.Any()).
				Return(sampleForwarderDto(), nil)
			mock.EXPECT().DeleteForwarder(gomock.Any(), testForwarderID).Return(nil)
			if tc.wantRun {
				answer := sampleForwarderDto()
				answer.Match = ptr(testMatchNext)
				mock.EXPECT().UpdateForwarder(gomock.Any(), testForwarderID, gomock.Any()).
					Return(answer, nil)
			}

			var updated bool
			integration.LifeCycleTest{
				Resource: tokens.Type(forwarderResourceType),
				Create:   integration.Operation{Inputs: forwarderInputs(nil)},
				Updates: []integration.Operation{{
					Inputs: tc.update,
					Hook:   func(_, _ property.Map) { updated = true },
				}},
			}.Run(t, mockedServer(t, mock))

			assert.Equal(t, tc.wantRun, updated,
				"the harness runs the update hook only when the diff plans a change")
		})
	}
}

func forwarderSchema(t *testing.T) *schemaResource {
	t.Helper()
	schema := getSchema(t)
	forwarder := schema.Resources[forwarderResourceType]
	require.NotNil(t, forwarder, "the schema exposes no %s", forwarderResourceType)
	return forwarder
}

// The inbox is the only input that replaces the forwarder, so it is the only input that carries the
// replacement warning.
func TestOnlyTheInboxForwarderInboxCarriesTheReplaceWarning(t *testing.T) {
	t.Parallel()
	forwarder := forwarderSchema(t)
	for name, prop := range forwarder.InputProperties {
		if name == forwarderPropInboxID {
			assert.Contains(t, prop.Description, "Warning:", "property %q", name)
			assert.Contains(t, prop.Description, "replace", "property %q", name)
			continue
		}
		assert.NotContains(t, prop.Description, "Warning:", "property %q", name)
	}
	assert.Contains(t, forwarder.Description, "in place",
		"the resource description must state that an option change updates the forwarder in place")
	assert.Contains(t, forwarder.Description, "replace",
		"the resource description must state that an inbox change replaces the forwarder")
}

// The name is the one property a stack cannot set. A reader who never opens the property list must
// still learn from the resource description that the server writes it.
func TestTheInboxForwarderDescriptionSaysYouCannotSetTheName(t *testing.T) {
	t.Parallel()
	description := forwarderSchema(t).Description
	assert.Contains(t, description, forwarderPropName,
		"the resource description must name the name property")
	assert.Contains(t, description, "cannot set",
		"the resource description must state that you cannot set the name")

	name := forwarderSchema(t).Properties[forwarderPropName]
	require.NotNil(t, name)
	assert.Contains(t, name.Description, "cannot set",
		"the output description must state that you cannot set the name")
}

func TestInboxForwarderStateCoversEveryInput(t *testing.T) {
	t.Parallel()
	forwarder := forwarderSchema(t)
	require.NotEmpty(t, forwarder.InputProperties)
	for name := range forwarder.InputProperties {
		assert.Contains(t, forwarder.Properties, name,
			"input property %q has no output of the same name, so the diff cannot read it from the state",
			name)
	}
}

// forwarderObjectType answers the members of one match-tree object and the ones the API requires.
func forwarderObjectType(t *testing.T, name string) (map[string]*schemaProperty, []string) {
	t.Helper()
	ty := schemaObjectType(t, name)
	return ty.Properties, ty.Required
}

func TestEveryInboxForwarderMatchTreePropertyHasADescription(t *testing.T) {
	t.Parallel()
	for name, want := range map[string][]string{
		forwarderMatchGroupType: {forwarderPropOperator},
		forwarderMatchRuleType:  {forwarderPropField, forwarderPropShould, forwarderPropValue},
	} {
		props, required := forwarderObjectType(t, name)
		require.NotEmpty(t, props)
		for member, prop := range props {
			assert.NotEmpty(t, prop.Description, "%s.%s has no description", name, member)
		}
		assert.ElementsMatch(t, want, required, "%s requires the wrong members", name)
	}
}

// The match tree nests, so the group type must point back at itself. The schema keeps one token for
// the group, and a walk that never terminated would never produce this reference.
func TestTheInboxForwarderMatchGroupReferencesItself(t *testing.T) {
	t.Parallel()
	group := schemaObjectType(t, forwarderMatchGroupType).Properties
	require.Contains(t, group, forwarderPropGroups)
	require.NotNil(t, group[forwarderPropGroups].Items)
	assert.Equal(t, "#/types/"+forwarderMatchGroupType, group[forwarderPropGroups].Items.Ref)

	require.NotNil(t, group[forwarderPropMatches].Items)
	assert.Equal(t, "#/types/"+forwarderMatchRuleType, group[forwarderPropMatches].Items.Ref)
}

// The values come from the pinned API spec at test time, so a vendor value the spec grows fails
// here instead of reaching a stack outside the enum.
func TestForwarderFieldEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	want := specEnumValues(t, "CreateInboxForwarderOptions", forwarderPropField)
	// One Go type serves the option, the read model and the nested rule, which holds while the
	// three spec schemas agree.
	assert.Equal(t, want, specEnumValues(t, "InboxForwarderDto", forwarderPropField))
	assert.Equal(t, want, specEnumValues(t, "InboxAutomationMatchOption", forwarderPropField))
	assertEnumValues(t, "mailslurp:index:ForwarderField", want)
}

func TestForwarderShouldEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	want := specEnumValues(t, "CreateInboxForwarderOptions", forwarderPropShould)
	assert.Equal(t, want, specEnumValues(t, "InboxForwarderDto", forwarderPropShould))
	assert.Equal(t, want, specEnumValues(t, "InboxAutomationMatchOption", forwarderPropShould))
	assertEnumValues(t, "mailslurp:index:ForwarderShould", want)
}

func TestForwarderMatchOperatorEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	assertEnumValues(t, "mailslurp:index:ForwarderMatchOperator",
		specEnumValues(t, "InboxAutomationMatchOptions", forwarderPropOperator))
}

// Forwarder naming and sweep policy. The API gives a forwarder no name a program can set, so the
// one forward address carries the run marker. This file has no build tag, so both tiers read these.
const testForwarderKind = "forwarder"

// forwarderRecipientFor builds the forward address of one run from a newTestName value. The host
// belongs to the reserved documentation domain, so no real mailbox can take a forwarded email.
func forwarderRecipientFor(name string) string { return name + "@example.com" }

// forwarderRecipientPattern matches only what forwarderRecipientFor builds, so an address this
// package did not build is never a sweep candidate.
var forwarderRecipientPattern = regexp.MustCompile(
	`^` + testNamePrefix + testForwarderKind + `-[0-9a-f]{8}@example\.com$`)

// isSweepableTestForwarder reports whether the sweeper may delete a forwarder. A blank identifier
// would reach the whole collection, and a young forwarder can belong to a concurrent run.
func isSweepableTestForwarder(id string, recipients []string, createdAt, now time.Time) bool {
	if id == "" || len(recipients) != 1 || !forwarderRecipientPattern.MatchString(recipients[0]) {
		return false
	}
	return now.Sub(createdAt) > sweepMinAge
}

// The integration sweeper deletes forwarders from a live account, so its predicate answers false
// for every address this package did not build, and for an identifier it did not capture.
func TestTheSweeperMatchesOnlyTheForwarderRecipientsTheTestsBuild(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	now := old.Add(24 * time.Hour)

	for _, address := range []string{
		forwarderRecipientFor(newTestName(testForwarderKind)),
		"pulumi-test-forwarder-00000000@example.com",
		"pulumi-test-forwarder-ffffffff@example.com",
	} {
		assert.True(t, isSweepableTestForwarder(testForwarderID, []string{address}, old, now),
			"must sweep %q", address)
	}

	for _, address := range []string{
		"", testForwardTo, testRuleValue, "pulumi-test-inbox-abcd1234@example.com",
		"pulumi-test-forwarder-ABCD1234@example.com",
		"pulumi-test-forwarder-abcd123@example.com",
		"pulumi-test-forwarder-abcd12345@example.com",
		"pulumi-test-forwarder-abcd1234@example.org",
		"pulumi-test-forwarder-abcd1234@example.com.test",
		" pulumi-test-forwarder-abcd1234@example.com",
		"pulumi-test-forwarder-abcd1234@example.com\n",
	} {
		assert.False(t, isSweepableTestForwarder(testForwarderID, []string{address}, old, now),
			"must not sweep %q", address)
	}

	sweepable := forwarderRecipientFor(newTestName(testForwarderKind))
	assert.False(t, isSweepableTestForwarder("", []string{sweepable}, old, now),
		"a blank identifier reaches the whole collection, so it is never a sweep candidate")
	assert.False(t, isSweepableTestForwarder(testForwarderID, []string{sweepable},
		now.Add(-time.Minute), now), "a fresh forwarder can belong to a concurrent run")
	assert.False(t, isSweepableTestForwarder(testForwarderID, nil, old, now),
		"a forwarder without a recipient carries no marker")
	assert.False(t, isSweepableTestForwarder(testForwarderID,
		[]string{sweepable, testForwardTo}, old, now),
		"a forwarder that sends anywhere else is not one of ours")
}

// Forwarder entitlement policy. The account plan decides whether the API builds a forwarder at all,
// so the decision and its test are hermetic and the integration probe reads them from here.

// errorCodeSubscriptionLimit is the code MailSlurp answers when the account plan does not carry the
// wanted feature. The forwarder create answers it with status 426 on an account without the feature.
const errorCodeSubscriptionLimit = "W_429_SUBSCRIPTION_LIMIT_REACHED"

// noForwarderEntitlementReason names the precondition the forwarder lifecycle needs. MailSlurp sells
// the feature per plan, so this suite cannot turn it on to run the legs.
const noForwarderEntitlementReason = "the MailSlurp account plan does not carry the inbox " +
	"forwarder feature: the create answered " + errorCodeSubscriptionLimit +
	", and GET /user/info reports subscriptionType null"

// skipReasonForForwarderEntitlement answers the reason to skip the forwarder lifecycle, or the empty
// string when the plan carries the feature. Every other answer runs the legs and reports itself.
func skipReasonForForwarderEntitlement(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode == errorCodeSubscriptionLimit {
		return noForwarderEntitlementReason
	}
	return ""
}

// An entitled account must run the forwarder lifecycle. Only the one plan-refusal code skips it, and
// this test pins that decision, because no test can buy the feature to prove it live.
func TestTheForwarderLifeCycleSkipsOnlyWhenThePlanRefusesTheFeature(t *testing.T) {
	t.Parallel()
	assert.Equal(t, noForwarderEntitlementReason, skipReasonForForwarderEntitlement(
		&APIError{StatusCode: http.StatusUpgradeRequired, ErrorCode: errorCodeSubscriptionLimit}),
		"the one plan-refusal code skips the lifecycle and names itself")

	assert.Empty(t, skipReasonForForwarderEntitlement(nil),
		"an entitled account must run the forwarder lifecycle")
	assert.Empty(t, skipReasonForForwarderEntitlement(&APIError{
		StatusCode: http.StatusBadRequest,
		ErrorCode:  "W_400_BAD_REQUEST",
		Message:    "Either field/match or matchOptions must be provided",
	}), "another API answer must fail the run and never skip it")
	assert.Empty(t, skipReasonForForwarderEntitlement(errors.New("the connection was refused")),
		"an error that is not an API answer must fail the run and never skip it")
}
