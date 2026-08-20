package internal

import (
	"context"
	"go/parser"
	"go/token"
	"net/http"
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
	templateResourceType       = "mailslurp:index:EmailTemplate"
	templateVariableSchemaType = "mailslurp:index:EmailTemplateVariable"

	testTemplateID       = "template-1"
	testTemplateIDNext   = "template-2"
	testTemplateName     = "the welcome letter"
	testTemplateNameNext = "the reminder letter"
	testTemplateContent  = "Hello {{firstName}}"
	testContentNext      = "Hello {{firstName}} and {{lastName}}"
	testTemplateTime     = "2026-08-18T15:24:48.111Z"
	testFirstVariable    = "firstName"
	testSecondVariable   = "lastName"
)

func templateURN() presource.URN {
	return presource.NewURN("test", "provider", "", tokens.Type(templateResourceType), "test")
}

// templateResource builds the resource with a mock client already in place, which is what Configure
// does in production.
func templateResource(t *testing.T) (*EmailTemplate, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &EmailTemplate{config: &Config{client: mock}}, mock
}

func sampleTemplateDto(names ...string) *TemplateDto {
	dto := &TemplateDto{
		ID:        testTemplateID,
		Name:      testTemplateName,
		Content:   testTemplateContent,
		CreatedAt: testTemplateTime,
	}
	for _, name := range names {
		dto.Variables = append(dto.Variables,
			TemplateVariable{Name: name, VariableType: templateVariableTypeString})
	}
	return dto
}

// templateVariableValues renders the server-parsed variable list the way the state carries it.
func templateVariableValues(names ...string) property.Value {
	out := make([]property.Value, 0, len(names))
	for _, name := range names {
		out = append(out, property.New(map[string]property.Value{
			templatePropName:         property.New(name),
			templatePropVariableType: property.New(templateVariableTypeString),
		}))
	}
	return property.New(out)
}

// priorTemplateState is the stored state the diff reads. Each case overrides the one property it
// exercises.
func priorTemplateState(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		templatePropName:      property.New(testTemplateName),
		templatePropContent:   property.New(testTemplateContent),
		templatePropVariables: templateVariableValues(testFirstVariable),
		templatePropCreatedAt: property.New(testTemplateTime),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// templateInputs always carries both inputs, because the API requires both on every write.
func templateInputs(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		templatePropName:    property.New(testTemplateName),
		templatePropContent: property.New(testTemplateContent),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

// diffTemplate runs a diff through the provider server, which is the path that proves the framework
// plans an update without a hand-written Diff.
func diffTemplate(t *testing.T, state, inputs property.Map) (p.DiffResponse, error) {
	t.Helper()
	s := newTestServer(t, nil)
	return s.Diff(p.DiffRequest{
		ID:     testTemplateID,
		Urn:    templateURN(),
		State:  state,
		Inputs: inputs,
	})
}

// Every input property, with a prior value and a changed value.
func templateCases() []propertyCase {
	return []propertyCase{
		{templatePropName, property.New(testTemplateName), property.New(testTemplateNameNext)},
		{templatePropContent, property.New(testTemplateContent), property.New(testContentNext)},
	}
}

// A change to `name` or to `content` updates the template in place.
func TestEmailTemplateUpdatesEveryInputInPlace(t *testing.T) {
	t.Parallel()
	for _, tc := range templateCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffTemplate(t,
				priorTemplateState(one(tc.property, tc.prior)),
				templateInputs(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a changed %s must produce a diff", tc.property)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.Update, got.Kind, "%s must update in place", tc.property)
			assert.Len(t, resp.DetailedDiff, 1, "only %s changed", tc.property)
		})
	}
}

// Nothing on a template forces a replacement. The case table must cover every input the
// schema takes, so a property this test never diffs cannot reach a stack unproven.
func TestNoEmailTemplatePropertyEverPlansAReplacement(t *testing.T) {
	t.Parallel()
	cases := templateCases()

	var covered []string
	for _, tc := range cases {
		covered = append(covered, tc.property)
	}
	template := templateSchema(t)
	var inputs []string
	for name := range template.InputProperties {
		inputs = append(inputs, name)
	}
	require.ElementsMatch(t, inputs, covered, "every input property must carry a diff case")

	for _, tc := range cases {
		resp, err := diffTemplate(t,
			priorTemplateState(one(tc.property, tc.prior)),
			templateInputs(one(tc.property, tc.changed)))
		require.NoError(t, err)
		for name, got := range resp.DetailedDiff {
			assert.NotEqual(t, p.AddReplace, got.Kind, "property %q", name)
			assert.NotEqual(t, p.UpdateReplace, got.Kind, "property %q", name)
			assert.NotEqual(t, p.DeleteReplace, got.Kind, "property %q", name)
		}
	}
}

func TestEmailTemplatePlansNothingWhenTheInputsMatchTheState(t *testing.T) {
	t.Parallel()
	resp, err := diffTemplate(t, priorTemplateState(nil), templateInputs(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
	assert.Empty(t, resp.DetailedDiff)
}

// The server parses `variables` out of the content, so it is an output. A list that
// changed in the API answer alone must plan no diff at all.
func TestAChangedVariablesListInTheAPIAnswerPlansNoDiff(t *testing.T) {
	t.Parallel()
	grown := priorTemplateState(one(templatePropVariables,
		templateVariableValues(testFirstVariable, testSecondVariable)))
	emptied := priorTemplateState(one(templatePropVariables, templateVariableValues()))

	for name, state := range map[string]property.Map{"a longer list": grown, "an empty list": emptied} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resp, err := diffTemplate(t, state, templateInputs(nil))
			require.NoError(t, err)
			assert.False(t, resp.HasChanges, "the variables of the API answer must never plan a diff")
			assert.Empty(t, resp.DetailedDiff)
		})
	}
}

// The diff reads the inputs alone, so `variables` can only stay out of it while the schema keeps it
// out of the inputs.
func TestTheEmailTemplateSchemaKeepsTheOutputsOutOfTheInputs(t *testing.T) {
	t.Parallel()
	template := templateSchema(t)
	for _, name := range []string{templatePropVariables, templatePropCreatedAt} {
		assert.Contains(t, template.Properties, name, "%q must be an output", name)
		assert.NotContains(t, template.InputProperties, name, "%q must never be an input", name)
	}
}

// Until a resource implements CustomUpdate, infer's default diff turns every change into a
// replacement. The template updates every property in place, so the Update must exist.
func TestTheEmailTemplateImplementsAnUpdate(t *testing.T) {
	t.Parallel()
	var resource any = &EmailTemplate{}
	_, isUpdatable := resource.(infer.CustomUpdate[EmailTemplateArgs, EmailTemplateState])
	assert.True(t, isUpdatable, "a template without an Update replaces on every change")
}

// A hand-written diff stops infer from reading the `replaceOnChanges` tags. The template
// excludes no property from the comparison, so the default diff serves it.
func TestTheEmailTemplateImplementsNoCustomDiff(t *testing.T) {
	t.Parallel()
	var resource any = &EmailTemplate{}
	_, isCustom := resource.(infer.CustomDiff[EmailTemplateArgs, EmailTemplateState])
	assert.False(t, isCustom, "the default diff computes the update kinds this resource wants")
}

// The default diff forces a replacement for every input the schema tags, so no input may carry one.
func TestTheEmailTemplateTagsNoInputAsReplaceOnChanges(t *testing.T) {
	t.Parallel()
	template := templateSchema(t)
	require.Len(t, template.InputProperties, len(templateCases()))
	for name, prop := range template.InputProperties {
		assert.False(t, prop.ReplaceOnChanges, "property %q must update in place", name)
	}
	assert.ElementsMatch(t, []string{templatePropName, templatePropContent}, template.RequiredInputs,
		"the API requires both properties on every write")
}

func TestEmailTemplateCreatePostsBothProperties(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)

	var got TemplateOptions
	mock.EXPECT().CreateTemplate(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts TemplateOptions) (*TemplateDto, error) {
			got = opts
			return sampleTemplateDto(testFirstVariable), nil
		})

	resp, err := template.Create(context.Background(), infer.CreateRequest[EmailTemplateArgs]{
		Inputs: EmailTemplateArgs{Name: testTemplateName, Content: testTemplateContent},
	})
	require.NoError(t, err)

	assert.Equal(t, testTemplateName, got.Name)
	assert.Equal(t, testTemplateContent, got.Content)

	assert.Equal(t, testTemplateID, resp.ID)
	assert.Equal(t, testTemplateName, resp.Output.Name)
	assert.Equal(t, testTemplateContent, resp.Output.Content)
	assert.Equal(t, testTemplateTime, resp.Output.CreatedAt)
	require.Len(t, resp.Output.Variables, 1)
	assert.Equal(t, testFirstVariable, resp.Output.Variables[0].Name)
	assert.Equal(t, templateVariableTypeString, resp.Output.Variables[0].VariableType)
}

// A template without a variable must answer an empty list, not a missing one: `variables` is a
// required output, and a null would reach the stack as a hole.
func TestEmailTemplateCreateAnswersAnEmptyVariableListWhenTheContentHasNone(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().CreateTemplate(gomock.Any(), gomock.Any()).Return(sampleTemplateDto(), nil)

	resp, err := template.Create(context.Background(), infer.CreateRequest[EmailTemplateArgs]{
		Inputs: EmailTemplateArgs{Name: testTemplateName, Content: testTemplateContent},
	})
	require.NoError(t, err)
	assert.NotNil(t, resp.Output.Variables)
	assert.Empty(t, resp.Output.Variables)
}

func TestEmailTemplateCreateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test if the preview reaches the API.
	template, _ := templateResource(t)

	resp, err := template.Create(context.Background(), infer.CreateRequest[EmailTemplateArgs]{
		Inputs: EmailTemplateArgs{Name: testTemplateNameNext, Content: testContentNext},
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "a preview must not claim an id")
	assert.Equal(t, testTemplateNameNext, resp.Output.Name)
	assert.Equal(t, testContentNext, resp.Output.Content)
	// `variables` is a required output, so the preview answers an empty list and never a null.
	assert.NotNil(t, resp.Output.Variables, "only the server parses the variables")
	assert.Empty(t, resp.Output.Variables)
}

// A provider that never ran Configure carries no configuration, and a Configure that failed can
// leave a configuration without a client. Every lifecycle method must refuse both.
func TestEveryEmailTemplateLifecycleMethodRefusesAnUnconfiguredProvider(t *testing.T) {
	t.Parallel()
	for title, template := range map[string]*EmailTemplate{
		titleWithoutConfiguration: {},
		titleWithoutClient:        {config: &Config{}},
	} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			_, err := template.Create(ctx, infer.CreateRequest[EmailTemplateArgs]{})
			requireNotConfigured(t, err)

			_, err = template.Read(ctx,
				infer.ReadRequest[EmailTemplateArgs, EmailTemplateState]{ID: testTemplateID})
			requireNotConfigured(t, err)

			_, err = template.Update(ctx,
				infer.UpdateRequest[EmailTemplateArgs, EmailTemplateState]{ID: testTemplateID})
			requireNotConfigured(t, err)

			_, err = template.Delete(ctx,
				infer.DeleteRequest[EmailTemplateState]{ID: testTemplateID})
			requireNotConfigured(t, err)
		})
	}
}

func TestEmailTemplateCreateReportsTheAPIError(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().CreateTemplate(gomock.Any(), gomock.Any()).
		Return(nil, &APIError{StatusCode: http.StatusBadRequest, Message: "the content is not valid"})

	_, err := template.Create(context.Background(), infer.CreateRequest[EmailTemplateArgs]{
		Inputs: EmailTemplateArgs{Name: testTemplateName, Content: testTemplateContent},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the content is not valid")
}

// Read serves a refresh and an import, so it answers the inputs as well as the state.
func TestEmailTemplateReadAnswersTheStateAndTheInputs(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().GetTemplate(gomock.Any(), testTemplateID).
		Return(sampleTemplateDto(testFirstVariable), nil)

	resp, err := template.Read(context.Background(),
		infer.ReadRequest[EmailTemplateArgs, EmailTemplateState]{ID: testTemplateID})
	require.NoError(t, err)

	assert.Equal(t, testTemplateID, resp.ID)
	assert.Equal(t, testTemplateName, resp.State.Name)
	assert.Equal(t, testTemplateContent, resp.State.Content)
	require.Len(t, resp.State.Variables, 1)
	assert.Equal(t, testFirstVariable, resp.State.Variables[0].Name)

	// An import starts with no input at all, so the inputs must come from the API answer.
	assert.Equal(t, testTemplateName, resp.Inputs.Name)
	assert.Equal(t, testTemplateContent, resp.Inputs.Content)
}

// A deleted template is drift. An empty response tells the engine the template is gone.
func TestEmailTemplateReadReportsATemplateThatIsGone(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().GetTemplate(gomock.Any(), testTemplateID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	resp, err := template.Read(context.Background(),
		infer.ReadRequest[EmailTemplateArgs, EmailTemplateState]{ID: testTemplateID})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "an empty response reports the template as gone")
}

// A 404 that carries any other code is a real error, because an unrouted path answers
// 404 as well, and dropping the template from the state would lose it.
func TestEmailTemplateReadReportsAnyOther404AsAnError(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().GetTemplate(gomock.Any(), testTemplateID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  "W_404_SOMETHING_ELSE",
		Message:    "the path is not routed",
	})

	_, err := template.Read(context.Background(),
		infer.ReadRequest[EmailTemplateArgs, EmailTemplateState]{ID: testTemplateID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the path is not routed")
}

// The update body is the whole create body, and the pinned spec marks both properties required, so
// a call that changed one property must still carry the other.
func TestEmailTemplateUpdateSendsBothPropertiesWhenOnlyTheContentChanged(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)

	answer := sampleTemplateDto(testFirstVariable, testSecondVariable)
	answer.Content = testContentNext

	var got TemplateOptions
	mock.EXPECT().UpdateTemplate(gomock.Any(), testTemplateID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts TemplateOptions) (*TemplateDto, error) {
			got = opts
			return answer, nil
		})

	resp, err := template.Update(context.Background(),
		infer.UpdateRequest[EmailTemplateArgs, EmailTemplateState]{
			ID:     testTemplateID,
			State:  EmailTemplateState{Name: testTemplateName, Content: testTemplateContent},
			Inputs: EmailTemplateArgs{Name: testTemplateName, Content: testContentNext},
		})
	require.NoError(t, err)

	assert.Equal(t, testTemplateName, got.Name)
	assert.Equal(t, testContentNext, got.Content)

	assert.Equal(t, testContentNext, resp.Output.Content)
	require.Len(t, resp.Output.Variables, 2, "the server parses the variables again on every write")
	assert.Equal(t, testSecondVariable, resp.Output.Variables[1].Name)
}

func TestEmailTemplateUpdateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test if the preview reaches the API.
	template, _ := templateResource(t)

	resp, err := template.Update(context.Background(),
		infer.UpdateRequest[EmailTemplateArgs, EmailTemplateState]{
			ID: testTemplateID,
			State: EmailTemplateState{
				Name:      testTemplateName,
				Content:   testTemplateContent,
				CreatedAt: testTemplateTime,
			},
			Inputs: EmailTemplateArgs{Name: testTemplateNameNext, Content: testContentNext},
			DryRun: true,
		})
	require.NoError(t, err)
	assert.Equal(t, testTemplateNameNext, resp.Output.Name)
	assert.Equal(t, testContentNext, resp.Output.Content)
	assert.Equal(t, testTemplateTime, resp.Output.CreatedAt, "an update never moves the creation time")
}

func TestEmailTemplateUpdateReportsTheAPIError(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().UpdateTemplate(gomock.Any(), testTemplateID, gomock.Any()).
		Return(nil, &APIError{StatusCode: http.StatusBadRequest, Message: "the name is not valid"})

	_, err := template.Update(context.Background(),
		infer.UpdateRequest[EmailTemplateArgs, EmailTemplateState]{
			ID:     testTemplateID,
			Inputs: EmailTemplateArgs{Name: testTemplateName, Content: testTemplateContent},
		})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the name is not valid")
}

func TestEmailTemplateDeleteRemovesTheTemplate(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().DeleteTemplate(gomock.Any(), testTemplateID).Return(nil)

	_, err := template.Delete(context.Background(),
		infer.DeleteRequest[EmailTemplateState]{ID: testTemplateID})
	require.NoError(t, err)
}

func TestEmailTemplateDeleteToleratesATemplateThatIsAlreadyGone(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().DeleteTemplate(gomock.Any(), testTemplateID).Return(&APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	_, err := template.Delete(context.Background(),
		infer.DeleteRequest[EmailTemplateState]{ID: testTemplateID})
	require.NoError(t, err)
}

func TestEmailTemplateDeleteReportsAnyOtherError(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().DeleteTemplate(gomock.Any(), testTemplateIDNext).Return(&APIError{
		StatusCode: http.StatusInternalServerError,
		Message:    "the server failed",
	})

	_, err := template.Delete(context.Background(),
		infer.DeleteRequest[EmailTemplateState]{ID: testTemplateIDNext})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the server failed")
}

// The mock refuses every call the test does not expect, so a lifecycle method that reached another
// endpoint fails here. The preview legs must reach no endpoint at all.
func TestEveryEmailTemplateLifecycleMethodCallsOneTemplateEndpointOnly(t *testing.T) {
	t.Parallel()
	template, mock := templateResource(t)
	mock.EXPECT().CreateTemplate(gomock.Any(), gomock.Any()).Return(sampleTemplateDto(), nil)
	mock.EXPECT().GetTemplate(gomock.Any(), testTemplateID).Return(sampleTemplateDto(), nil)
	mock.EXPECT().UpdateTemplate(gomock.Any(), testTemplateID, gomock.Any()).
		Return(sampleTemplateDto(), nil)
	mock.EXPECT().DeleteTemplate(gomock.Any(), testTemplateID).Return(nil)

	ctx := context.Background()
	inputs := EmailTemplateArgs{Name: testTemplateName, Content: testTemplateContent}

	_, err := template.Create(ctx, infer.CreateRequest[EmailTemplateArgs]{Inputs: inputs})
	require.NoError(t, err)
	_, err = template.Read(ctx,
		infer.ReadRequest[EmailTemplateArgs, EmailTemplateState]{ID: testTemplateID})
	require.NoError(t, err)
	_, err = template.Update(ctx, infer.UpdateRequest[EmailTemplateArgs, EmailTemplateState]{
		ID: testTemplateID, Inputs: inputs,
	})
	require.NoError(t, err)
	_, err = template.Delete(ctx, infer.DeleteRequest[EmailTemplateState]{ID: testTemplateID})
	require.NoError(t, err)
}

// A source scan reads spellings, so a request built from parts evades it. The import set is
// structure: with no transport package in reach, the resource speaks through the client alone.
func TestTheEmailTemplateSourceReachesTheAPIThroughTheClientAlone(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "emailtemplate.go", nil, parser.ImportsOnly)
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

func templateSchema(t *testing.T) *schemaResource {
	t.Helper()
	schema := getSchema(t)
	template := schema.Resources[templateResourceType]
	require.NotNil(t, template, "the schema exposes no %s", templateResourceType)
	return template
}

// The template is the mirror of the ruleset: no property replaces it, so no description may carry
// the replacement warning that a replace-forcing property needs.
func TestNoEmailTemplateDescriptionCarriesAReplacementWarning(t *testing.T) {
	t.Parallel()
	template := templateSchema(t)
	for name, prop := range template.InputProperties {
		assert.NotContains(t, prop.Description, "Warning:", "property %q", name)
	}
	assert.Contains(t, template.Description, "in place",
		"the resource description must state that a change updates the template in place")
}

// The variables are the one property a stack cannot set. A reader who never opens the property list
// must still learn from the resource description that the server writes them.
func TestTheEmailTemplateDescriptionSaysTheServerParsesTheVariables(t *testing.T) {
	t.Parallel()
	description := templateSchema(t).Description
	assert.Contains(t, description, templatePropVariables,
		"the resource description must name the variables property")
	assert.Contains(t, description, "parses",
		"the resource description must state that MailSlurp parses the variables")
	assert.Contains(t, description, "cannot set",
		"the resource description must state that you cannot set the variables")
}

func TestEmailTemplateStateCoversEveryInput(t *testing.T) {
	t.Parallel()
	template := templateSchema(t)
	require.NotEmpty(t, template.InputProperties)
	for name := range template.InputProperties {
		assert.Contains(t, template.Properties, name,
			"input property %q has no output of the same name, so the diff cannot read it from the state", name)
	}
}

// templateVariableProperties answers the members of the variable object, and checks that the API
// requires the two the resource ships.
func templateVariableProperties(t *testing.T) map[string]*schemaProperty {
	t.Helper()
	ty := schemaObjectType(t, templateVariableSchemaType)
	assert.ElementsMatch(t, []string{templatePropName, templatePropVariableType}, ty.Required)
	return ty.Properties
}

func TestEveryEmailTemplateVariablePropertyHasADescription(t *testing.T) {
	t.Parallel()
	props := templateVariableProperties(t)
	require.Len(t, props, 2)
	for name, prop := range props {
		assert.NotEmpty(t, prop.Description, "variable property %q has no description", name)
	}
}

// The one documented value stays a plain string, so a value MailSlurp adds later reaches a
// stack instead of breaking it. The value itself comes from the pinned spec, never from a hand list.
func TestTheEmailTemplateVariableTypeIsAPlainStringThatNamesItsOnlyValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{templateVariableTypeString},
		specEnumValues(t, "TemplateVariable", templatePropVariableType))

	schema := getSchema(t)
	assert.NotContains(t, schema.Types, "mailslurp:index:TemplateVariableType")

	props := templateVariableProperties(t)
	assert.Contains(t, props[templatePropVariableType].Description, templateVariableTypeString)
}

// Template naming and sweep policy. A template carries a name, so the name carries the
// run marker. The integration sweeper reads these, and this file carries no build tag.
const testTemplateKind = "template"

// testTemplateNamePattern matches only what newTestName builds, so a template this package did not
// build is never a sweep candidate.
var testTemplateNamePattern = regexp.MustCompile(
	`^` + testNamePrefix + testTemplateKind + `-[0-9a-f]{8}$`)

// isSweepableTestTemplate reports whether the sweeper may delete a template. A blank identifier
// would reach the whole collection, and a young template can belong to a concurrent run.
func isSweepableTestTemplate(id, name string, createdAt, now time.Time) bool {
	if id == "" || !testTemplateNamePattern.MatchString(name) {
		return false
	}
	return now.Sub(createdAt) > sweepMinAge
}

// The integration sweeper deletes templates from a live account, so its predicate answers false for
// every name this package did not build, and for an identifier it did not capture.
func TestTheSweeperMatchesOnlyTheTemplateNamesTheTestsBuild(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	now := old.Add(24 * time.Hour)

	for _, name := range []string{
		newTestName(testTemplateKind),
		testNamePrefix + testTemplateKind + "-00000000",
		testNamePrefix + testTemplateKind + "-ffffffff",
	} {
		assert.True(t, isSweepableTestTemplate(testTemplateID, name, old, now), "must sweep %q", name)
	}

	for _, name := range []string{
		"", testTemplateName, "a real template", testNamePrefix + "inbox-abcd1234",
		testNamePrefix + testTemplateKind + "-ABCD1234",
		testNamePrefix + testTemplateKind + "-abcd123",
		testNamePrefix + testTemplateKind + "-abcd12345",
		" " + testNamePrefix + testTemplateKind + "-abcd1234",
		testNamePrefix + testTemplateKind + "-abcd1234\n",
		testNamePrefix + testTemplateKind + "-abcd1234-extra",
	} {
		assert.False(t, isSweepableTestTemplate(testTemplateID, name, old, now), "must not sweep %q", name)
	}

	sweepable := newTestName(testTemplateKind)
	assert.False(t, isSweepableTestTemplate("", sweepable, old, now),
		"a blank identifier reaches the whole collection, so it is never a sweep candidate")
	assert.False(t, isSweepableTestTemplate(testTemplateID, sweepable, now.Add(-time.Minute), now),
		"a fresh template can belong to a concurrent run")
}
