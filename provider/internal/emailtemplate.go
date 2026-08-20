package internal

import (
	"context"
	"errors"

	_ "embed"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// Schema property names of the EmailTemplate diff contract.
const (
	templatePropName         = "name"
	templatePropContent      = "content"
	templatePropVariables    = "variables"
	templatePropVariableType = "variableType"
	templatePropCreatedAt    = "createdAt"
)

// templateVariableTypeString is the only variable type the API reports. It stays a plain string in
// the schema, so a value MailSlurp adds later reaches a stack instead of breaking it.
const templateVariableTypeString = "STRING"

// emailTemplateExamples is the registry example page that make docs writes from docs/yaml.
//
//go:embed embed/emailtemplate-examples.md
var emailTemplateExamples string

// EmailTemplate is the MailSlurp email template resource.
type EmailTemplate struct {
	config *Config
}

// Annotate provides the user-facing description of the EmailTemplate resource.
func (t *EmailTemplate) Annotate(a infer.Annotator) {
	a.Describe(&t, dedent(`
		An email template that MailSlurp fills with values when you send an email.
		You write each variable inside the content with the moustache markers.

		You can update the "name" and the "content" of a template in place. Pulumi
		never replaces a template. MailSlurp parses the "variables" list from the
		content, so you cannot set it. A template is free, and it never counts
		against the inbox creation quota.
	`)+"\n\n"+emailTemplateExamples)
}

// EmailTemplateArgs are the inputs of the EmailTemplate resource. The API requires both properties
// on every write, and a change to either one updates the template in place.
type EmailTemplateArgs struct {
	Name    string `pulumi:"name"`
	Content string `pulumi:"content"`
}

// Annotate provides the user-facing descriptions of the EmailTemplate inputs.
func (ta *EmailTemplateArgs) Annotate(a infer.Annotator) {
	a.Describe(&ta.Name, dedent(`
		The display name of the template. You can update this property in place.
	`))
	a.Describe(&ta.Content, dedent(`
		The body of the template. Write a variable as "{{firstName}}", and
		MailSlurp reports each variable that it reads in the "variables" property.
		You can update this property in place.
	`))
}

// EmailTemplateVariable is one variable that the server parsed out of the template content.
type EmailTemplateVariable struct {
	Name         string `pulumi:"name"`
	VariableType string `pulumi:"variableType"`
}

// Annotate provides the user-facing descriptions of one parsed variable.
func (tv *EmailTemplateVariable) Annotate(a infer.Annotator) {
	a.Describe(&tv.Name, "The name of the variable, without the moustache markers.")
	a.Describe(&tv.VariableType, "The type of the variable. The API reports one value, `STRING`.")
}

// EmailTemplateState is the stored state of the EmailTemplate resource.
type EmailTemplateState struct {
	Name      string                  `pulumi:"name"`
	Content   string                  `pulumi:"content"`
	Variables []EmailTemplateVariable `pulumi:"variables"`
	CreatedAt string                  `pulumi:"createdAt"`
}

// Annotate provides the user-facing descriptions of the EmailTemplate outputs.
func (ts *EmailTemplateState) Annotate(a infer.Annotator) {
	a.Describe(&ts.Name, "The display name of the template.")
	a.Describe(&ts.Content, "The body of the template.")
	a.Describe(&ts.Variables, dedent(`
		The variables that MailSlurp reads from the content. MailSlurp parses this
		list on the server, so you cannot set it.
	`))
	a.Describe(&ts.CreatedAt, "The time when MailSlurp created the template.")
}

func (t *EmailTemplate) client() (Client, error) {
	if t.config == nil || t.config.client == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return nil, errors.New(notConfiguredMessage)
	}
	return t.config.client, nil
}

// templateOptions builds the whole body. The API takes the create options on the update as well,
// and it marks both properties required, so every write carries both.
func templateOptions(in EmailTemplateArgs) TemplateOptions {
	return TemplateOptions(in)
}

// templateVariablesFrom maps the parsed variable list of an API response. The schema property is
// required, so a template without a variable answers an empty list rather than a hole.
func templateVariablesFrom(parsed []TemplateVariable) []EmailTemplateVariable {
	out := make([]EmailTemplateVariable, 0, len(parsed))
	for _, variable := range parsed {
		out = append(out, EmailTemplateVariable(variable))
	}
	return out
}

// templateStateFrom maps an API response onto the stored state. The response carries every stored
// property, so no property travels from the prior state.
func templateStateFrom(dto *TemplateDto) EmailTemplateState {
	return EmailTemplateState{
		Name:      dto.Name,
		Content:   dto.Content,
		Variables: templateVariablesFrom(dto.Variables),
		CreatedAt: dto.CreatedAt,
	}
}

// templateInputsFrom answers the inputs that produce the state. An import starts with no input, so
// the refreshed state is the only source of them.
func templateInputsFrom(state EmailTemplateState) EmailTemplateArgs {
	return EmailTemplateArgs{Name: state.Name, Content: state.Content}
}

// applyTemplateInputs writes the wanted state onto a state value, for a preview that calls no API.
// The variables keep what the server last parsed, because only the server parses them.
func applyTemplateInputs(state EmailTemplateState, in EmailTemplateArgs) EmailTemplateState {
	state.Name = in.Name
	state.Content = in.Content
	if state.Variables == nil {
		state.Variables = []EmailTemplateVariable{}
	}
	return state
}

// Check refuses a blank required property before the create reaches MailSlurp.
func (t *EmailTemplate) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[EmailTemplateArgs], error) {
	return checkRequiredInputs[EmailTemplateArgs](ctx, req, templatePropName, templatePropContent)
}

// Create builds one template with POST /templates. A template belongs to the account, so it needs
// no inbox and it spends no inbox quota.
func (t *EmailTemplate) Create(
	ctx context.Context, req infer.CreateRequest[EmailTemplateArgs],
) (infer.CreateResponse[EmailTemplateState], error) {
	if req.DryRun {
		return infer.CreateResponse[EmailTemplateState]{
			Output: applyTemplateInputs(EmailTemplateState{}, req.Inputs),
		}, nil
	}

	client, err := t.client()
	if err != nil {
		return infer.CreateResponse[EmailTemplateState]{}, err
	}

	dto, err := client.CreateTemplate(ctx, templateOptions(req.Inputs))
	if err != nil {
		return infer.CreateResponse[EmailTemplateState]{}, err
	}

	return infer.CreateResponse[EmailTemplateState]{ID: dto.ID, Output: templateStateFrom(dto)}, nil
}

// Read refreshes from GET /templates/{templateId}. It answers the inputs as well, so a refresh
// reports drift on both properties and `pulumi import` builds a complete resource.
func (t *EmailTemplate) Read(
	ctx context.Context, req infer.ReadRequest[EmailTemplateArgs, EmailTemplateState],
) (infer.ReadResponse[EmailTemplateArgs, EmailTemplateState], error) {
	client, err := t.client()
	if err != nil {
		return infer.ReadResponse[EmailTemplateArgs, EmailTemplateState]{}, err
	}

	dto, err := client.GetTemplate(ctx, req.ID)
	if err != nil {
		// A deleted template is drift. An empty response tells the engine the template is gone.
		if IsGone(err) {
			return infer.ReadResponse[EmailTemplateArgs, EmailTemplateState]{}, nil
		}
		return infer.ReadResponse[EmailTemplateArgs, EmailTemplateState]{}, err
	}

	state := templateStateFrom(dto)
	return infer.ReadResponse[EmailTemplateArgs, EmailTemplateState]{
		ID:     req.ID,
		Inputs: templateInputsFrom(state),
		State:  state,
	}, nil
}

// Update changes both properties in place with PUT /templates/{templateId}. Without this method
// infer's default diff replaces on every change (pulumi-go-provider infer/resource.go:1142).
func (t *EmailTemplate) Update(
	ctx context.Context, req infer.UpdateRequest[EmailTemplateArgs, EmailTemplateState],
) (infer.UpdateResponse[EmailTemplateState], error) {
	if req.DryRun {
		return infer.UpdateResponse[EmailTemplateState]{
			Output: applyTemplateInputs(req.State, req.Inputs),
		}, nil
	}

	client, err := t.client()
	if err != nil {
		return infer.UpdateResponse[EmailTemplateState]{}, err
	}

	dto, err := client.UpdateTemplate(ctx, req.ID, templateOptions(req.Inputs))
	if err != nil {
		return infer.UpdateResponse[EmailTemplateState]{}, err
	}

	return infer.UpdateResponse[EmailTemplateState]{Output: templateStateFrom(dto)}, nil
}

// Delete removes the template. A template that is already gone is a success, not a failure.
func (t *EmailTemplate) Delete(
	ctx context.Context, req infer.DeleteRequest[EmailTemplateState],
) (infer.DeleteResponse, error) {
	client, err := t.client()
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if err := client.DeleteTemplate(ctx, req.ID); err != nil && !IsGone(err) {
		return infer.DeleteResponse{}, err
	}
	return infer.DeleteResponse{}, nil
}
