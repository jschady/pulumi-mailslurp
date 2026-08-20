package internal

import (
	"context"
	"errors"

	_ "embed"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// ForwarderField selects the part of the email that MailSlurp compares.
type ForwarderField string

// The comparison targets of the pinned API spec, in api/openapi.json.
const (
	ForwarderFieldRecipients         ForwarderField = "RECIPIENTS"
	ForwarderFieldSender             ForwarderField = "SENDER"
	ForwarderFieldSubject            ForwarderField = "SUBJECT"
	ForwarderFieldAttachments        ForwarderField = "ATTACHMENTS"
	ForwarderFieldAttachmentFilename ForwarderField = "ATTACHMENT_FILENAME"
	ForwarderFieldAttachmentText     ForwarderField = "ATTACHMENT_TEXT"
)

// Values lists the allowed comparison targets for the schema.
func (ForwarderField) Values() []infer.EnumValue[ForwarderField] {
	return []infer.EnumValue[ForwarderField]{
		{Name: "Recipients", Value: ForwarderFieldRecipients,
			Description: "MailSlurp compares the addresses that the email went to."},
		{Name: "Sender", Value: ForwarderFieldSender,
			Description: "MailSlurp compares the address that sent the email."},
		{Name: "Subject", Value: ForwarderFieldSubject,
			Description: "MailSlurp compares the subject of the email."},
		{Name: "Attachments", Value: ForwarderFieldAttachments,
			Description: "MailSlurp compares the attachments of the email."},
		{Name: "AttachmentFilename", Value: ForwarderFieldAttachmentFilename,
			Description: "MailSlurp compares the file name of each attachment."},
		{Name: "AttachmentText", Value: ForwarderFieldAttachmentText,
			Description: "MailSlurp compares the text inside each attachment."},
	}
}

// ForwarderShould selects the way MailSlurp compares the match value.
type ForwarderShould string

// The comparison modes of the pinned API spec, in api/openapi.json.
const (
	ForwarderShouldWildcard ForwarderShould = "WILDCARD"
	ForwarderShouldMatch    ForwarderShould = "MATCH"
	ForwarderShouldContain  ForwarderShould = "CONTAIN"
	ForwarderShouldEqual    ForwarderShould = "EQUAL"
)

// Values lists the allowed comparison modes for the schema.
func (ForwarderShould) Values() []infer.EnumValue[ForwarderShould] {
	return []infer.EnumValue[ForwarderShould]{
		{Name: "Wildcard", Value: ForwarderShouldWildcard,
			Description: "MailSlurp reads the match value as a pattern. A star matches every character."},
		{Name: "Match", Value: ForwarderShouldMatch,
			Description: "MailSlurp compares the part with the whole match value. " +
				"The API does not state how this mode differs from `EQUAL`."},
		{Name: "Contain", Value: ForwarderShouldContain,
			Description: "MailSlurp forwards the email when the part holds the match value inside it."},
		{Name: "Equal", Value: ForwarderShouldEqual,
			Description: "MailSlurp forwards the email when the part and the match value are the same."},
	}
}

// ForwarderMatchOperator selects the way MailSlurp joins the rules of one match group.
type ForwarderMatchOperator string

// The group operators of the pinned API spec, in api/openapi.json.
const (
	ForwarderMatchOperatorAnd ForwarderMatchOperator = "AND"
	ForwarderMatchOperatorOr  ForwarderMatchOperator = "OR"
)

// Values lists the allowed group operators for the schema.
func (ForwarderMatchOperator) Values() []infer.EnumValue[ForwarderMatchOperator] {
	return []infer.EnumValue[ForwarderMatchOperator]{
		{Name: "And", Value: ForwarderMatchOperatorAnd,
			Description: "MailSlurp forwards the email when every rule of the group matches."},
		{Name: "Or", Value: ForwarderMatchOperatorOr,
			Description: "MailSlurp forwards the email when one rule of the group matches."},
	}
}

// Schema property names of the InboxForwarder diff contract.
const (
	forwarderPropInboxID             = "inboxId"
	forwarderPropForwardToRecipients = "forwardToRecipients"
	forwarderPropField               = "field"
	forwarderPropShould              = "should"
	forwarderPropMatch               = "match"
	forwarderPropMatchOptions        = "matchOptions"
	forwarderPropName                = "name"
	forwarderPropCreatedAt           = "createdAt"
)

// InboxForwarderMatchRule is one leaf rule of the match tree.
type InboxForwarderMatchRule struct {
	Field  ForwarderField  `pulumi:"field"`
	Should ForwarderShould `pulumi:"should"`
	Value  string          `pulumi:"value"`
}

// Annotate provides the user-facing descriptions of one match rule.
func (fr *InboxForwarderMatchRule) Annotate(a infer.Annotator) {
	a.Describe(&fr.Field, "The part of the email that MailSlurp compares against the `value` property.")
	a.Describe(&fr.Should, "The way that MailSlurp compares the `value` property.")
	a.Describe(&fr.Value, "The value or the pattern that MailSlurp compares.")
}

// InboxForwarderMatchGroup is one node of the match tree. A group holds rules, and it holds groups,
// so the API model of the tree nests to any depth.
type InboxForwarderMatchGroup struct {
	Operator ForwarderMatchOperator     `pulumi:"operator"`
	Matches  []InboxForwarderMatchRule  `pulumi:"matches,optional"`
	Groups   []InboxForwarderMatchGroup `pulumi:"groups,optional"`
}

// Annotate provides the user-facing descriptions of one match group.
func (fg *InboxForwarderMatchGroup) Annotate(a infer.Annotator) {
	a.Describe(&fg.Operator, dedent(`
		The way that MailSlurp joins the rules of this group. Use "AND" when
		every rule must match. Use "OR" when one rule is enough.
	`))
	a.Describe(&fg.Matches, "The rules that MailSlurp applies to the email.")
	a.Describe(&fg.Groups, dedent(`
		The groups that this group holds. Put a group inside a group to build a
		deeper tree.
	`))
}

// inboxForwarderExamples is the registry example page that make docs writes from docs/yaml.
//
//go:embed embed/inboxforwarder-examples.md
var inboxForwarderExamples string

// InboxForwarder is the MailSlurp inbox forwarder resource.
type InboxForwarder struct {
	config *Config
}

// Annotate provides the user-facing description of the InboxForwarder resource.
func (f *InboxForwarder) Annotate(a infer.Annotator) {
	a.Describe(&f, dedent(`
		An inbox forwarder that sends a copy of a matching email to other
		addresses. MailSlurp reads each email that the inbox receives, and
		forwards the email when the rule matches.

		You can update the "field", the "should", the "match", the "matchOptions"
		and the "forwardToRecipients" properties in place. Pulumi replaces the
		forwarder when you change the "inboxId" property, because the API takes
		the inbox on the create call alone.

		The API requires the "field" and the "match" properties, or the
		"matchOptions" property. A write that names none of them fails. Each
		update sends the whole create body, so a value that you set outside
		Pulumi can go away.

		MailSlurp writes the "name" of the forwarder. The create call takes no
		name, so you cannot set it. This resource does not carry the
		"attachmentTextExtractionMethod" property of the API.
	`)+"\n\n"+inboxForwarderExamples)
}

// InboxForwarderArgs are the inputs of the InboxForwarder resource. The five options travel in the
// body of every write, and the inbox travels in the query of the create call alone.
type InboxForwarderArgs struct {
	InboxID             string                    `pulumi:"inboxId" provider:"replaceOnChanges"`
	ForwardToRecipients []string                  `pulumi:"forwardToRecipients"`
	Field               *ForwarderField           `pulumi:"field,optional"`
	Should              *ForwarderShould          `pulumi:"should,optional"`
	Match               *string                   `pulumi:"match,optional"`
	MatchOptions        *InboxForwarderMatchGroup `pulumi:"matchOptions,optional"`
}

// Annotate provides the user-facing descriptions of the InboxForwarder inputs.
func (fa *InboxForwarderArgs) Annotate(a infer.Annotator) {
	a.Describe(&fa.InboxID, dedent(`
		The inbox that the forwarder reads. MailSlurp compares each email that
		this inbox receives. **Warning:** If you change this property, Pulumi
		replaces the forwarder. The new forwarder carries a new identifier.
	`))
	a.Describe(&fa.ForwardToRecipients, dedent(`
		The addresses that MailSlurp sends the matching email to. The API
		requires this property on every write. You can update this property in
		place.
	`))
	a.Describe(&fa.Field, dedent(`
		The part of the email that MailSlurp compares against the "match" value.
		You can update this property in place.
	`))
	a.Describe(&fa.Should, dedent(`
		The way that MailSlurp compares the "match" value. Use "WILDCARD" for a
		pattern that holds a star. Use "EQUAL" for one exact value. You can
		update this property in place.
	`))
	a.Describe(&fa.Match, dedent(`
		The value or the pattern that MailSlurp compares. Use "*@example.com" to
		match every address of a domain. You can update this property in place.
	`))
	a.Describe(&fa.MatchOptions, dedent(`
		A tree of match rules that MailSlurp applies to each email. The
		"operator" property joins the rules of one group. You can put a group
		inside the "groups" property. You can update this property in place.
	`))
}

// InboxForwarderState is the stored state of the InboxForwarder resource.
type InboxForwarderState struct {
	InboxID             string                    `pulumi:"inboxId"`
	ForwardToRecipients []string                  `pulumi:"forwardToRecipients"`
	Field               *ForwarderField           `pulumi:"field,optional"`
	Should              *ForwarderShould          `pulumi:"should,optional"`
	Match               *string                   `pulumi:"match,optional"`
	MatchOptions        *InboxForwarderMatchGroup `pulumi:"matchOptions,optional"`
	Name                *string                   `pulumi:"name,optional"`
	CreatedAt           string                    `pulumi:"createdAt"`
}

// Annotate provides the user-facing descriptions of the InboxForwarder outputs.
func (fs *InboxForwarderState) Annotate(a infer.Annotator) {
	a.Describe(&fs.InboxID, "The inbox that the forwarder reads.")
	a.Describe(&fs.ForwardToRecipients, "The addresses that MailSlurp sends the matching email to.")
	a.Describe(&fs.Field, "The part of the email that MailSlurp compares.")
	a.Describe(&fs.Should, "The way that MailSlurp compares the `match` value.")
	a.Describe(&fs.Match, "The value or the pattern that MailSlurp compares.")
	a.Describe(&fs.MatchOptions, "The tree of match rules that MailSlurp applies to each email.")
	a.Describe(&fs.Name, dedent(`
		The display name of the forwarder. MailSlurp writes it, and the create
		call takes no name, so you cannot set it.
	`))
	a.Describe(&fs.CreatedAt, "The time when MailSlurp created the forwarder.")
}

func (f *InboxForwarder) client() (Client, error) {
	if f.config == nil || f.config.client == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return nil, errors.New(notConfiguredMessage)
	}
	return f.config.client, nil
}

// stringOf renders an optional enum as the optional string the request body carries.
func stringOf[T ~string](v *T) *string {
	if v == nil || *v == "" {
		return nil
	}
	return ptr(string(*v))
}

// enumOf reads an optional enum property of an API response. The API answers the empty string for a
// property that holds no value, so the empty string and the absent property mean the same thing.
func enumOf[T ~string](v *string) *T {
	if v == nil || *v == "" {
		return nil
	}
	return ptr(T(*v))
}

// recipientsOrEmpty answers the required address list. A required property must never be a hole in
// the state, and the API wants the property on every write.
func recipientsOrEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// matchGroupOf renders one match tree as the body the API takes. An empty list stays absent, so a
// group the program left out never reaches the request as a value.
func matchGroupOf(group *InboxForwarderMatchGroup) *ForwarderMatchOptions {
	if group == nil {
		return nil
	}
	out := &ForwarderMatchOptions{Operator: string(group.Operator)}
	for _, rule := range group.Matches {
		out.Matches = append(out.Matches, ForwarderMatch{
			Field:  string(rule.Field),
			Should: string(rule.Should),
			Value:  rule.Value,
		})
	}
	for i := range group.Groups {
		out.Groups = append(out.Groups, *matchGroupOf(&group.Groups[i]))
	}
	return out
}

// matchGroupFrom reads one match tree of an API response. An empty list and an absent list mean the
// same thing, so both answer nothing and an input that names neither plans no diff.
func matchGroupFrom(group *ForwarderMatchOptions) *InboxForwarderMatchGroup {
	if group == nil {
		return nil
	}
	out := &InboxForwarderMatchGroup{Operator: ForwarderMatchOperator(group.Operator)}
	for _, rule := range group.Matches {
		out.Matches = append(out.Matches, InboxForwarderMatchRule{
			Field:  ForwarderField(rule.Field),
			Should: ForwarderShould(rule.Should),
			Value:  rule.Value,
		})
	}
	for i := range group.Groups {
		out.Groups = append(out.Groups, *matchGroupFrom(&group.Groups[i]))
	}
	return out
}

// forwarderOptions builds the whole body. The API takes the create options on the update as well,
// so every write carries every option.
func forwarderOptions(in InboxForwarderArgs) ForwarderOptions {
	return ForwarderOptions{
		Field:               stringOf(in.Field),
		Should:              stringOf(in.Should),
		Match:               text(in.Match),
		ForwardToRecipients: recipientsOrEmpty(in.ForwardToRecipients),
		MatchOptions:        matchGroupOf(in.MatchOptions),
	}
}

// forwarderStateFrom maps an API response onto the stored state. The API marks `inboxId` nullable,
// so the inbox the caller named stands when the answer omits it: a blank input plans a replacement.
func forwarderStateFrom(dto *ForwarderDto, inboxID string) InboxForwarderState {
	state := InboxForwarderState{
		InboxID:             inboxID,
		ForwardToRecipients: recipientsOrEmpty(dto.ForwardToRecipients),
		Field:               enumOf[ForwarderField](dto.Field),
		Should:              enumOf[ForwarderShould](dto.Should),
		Match:               text(dto.Match),
		MatchOptions:        matchGroupFrom(dto.MatchOptions),
		Name:                text(dto.Name),
		CreatedAt:           dto.CreatedAt,
	}
	if reported := text(dto.InboxID); reported != nil {
		state.InboxID = *reported
	}
	return state
}

// forwarderInputsFrom answers the inputs that produce the state. An import starts with no input, so
// the refreshed state is the only source of them.
func forwarderInputsFrom(state InboxForwarderState) InboxForwarderArgs {
	return InboxForwarderArgs{
		InboxID:             state.InboxID,
		ForwardToRecipients: state.ForwardToRecipients,
		Field:               state.Field,
		Should:              state.Should,
		Match:               state.Match,
		MatchOptions:        state.MatchOptions,
	}
}

// applyForwarderInputs writes the wanted state onto a state value, for a preview that calls no API.
// The name keeps what the server last wrote, because only the server writes it.
func applyForwarderInputs(state InboxForwarderState, in InboxForwarderArgs) InboxForwarderState {
	state.InboxID = in.InboxID
	state.ForwardToRecipients = recipientsOrEmpty(in.ForwardToRecipients)
	state.Field = in.Field
	state.Should = in.Should
	state.Match = text(in.Match)
	state.MatchOptions = in.MatchOptions
	return state
}

// Check refuses a blank required property before the create reaches MailSlurp.
func (f *InboxForwarder) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[InboxForwarderArgs], error) {
	return checkRequiredInputs[InboxForwarderArgs](ctx, req,
		forwarderPropInboxID, forwarderPropForwardToRecipients)
}

// Create builds one forwarder for one inbox with `POST /forwarders?inboxId=`. The inbox always
// travels: a create that names none builds a forwarder for the whole account instead.
func (f *InboxForwarder) Create(
	ctx context.Context, req infer.CreateRequest[InboxForwarderArgs],
) (infer.CreateResponse[InboxForwarderState], error) {
	if req.DryRun {
		return infer.CreateResponse[InboxForwarderState]{
			Output: applyForwarderInputs(InboxForwarderState{}, req.Inputs),
		}, nil
	}

	client, err := f.client()
	if err != nil {
		return infer.CreateResponse[InboxForwarderState]{}, err
	}

	dto, err := client.CreateForwarder(ctx, req.Inputs.InboxID, forwarderOptions(req.Inputs))
	if err != nil {
		return infer.CreateResponse[InboxForwarderState]{}, err
	}

	return infer.CreateResponse[InboxForwarderState]{
		ID:     dto.ID,
		Output: forwarderStateFrom(dto, req.Inputs.InboxID),
	}, nil
}

// Read refreshes from `GET /forwarders/{id}`. It answers the inputs as well, so a refresh reports
// drift on every option and `pulumi import` builds a complete resource.
func (f *InboxForwarder) Read(
	ctx context.Context, req infer.ReadRequest[InboxForwarderArgs, InboxForwarderState],
) (infer.ReadResponse[InboxForwarderArgs, InboxForwarderState], error) {
	client, err := f.client()
	if err != nil {
		return infer.ReadResponse[InboxForwarderArgs, InboxForwarderState]{}, err
	}

	dto, err := client.GetForwarder(ctx, req.ID)
	if err != nil {
		// A deleted forwarder is drift. An empty response tells the engine the forwarder is gone.
		if IsGone(err) {
			return infer.ReadResponse[InboxForwarderArgs, InboxForwarderState]{}, nil
		}
		return infer.ReadResponse[InboxForwarderArgs, InboxForwarderState]{}, err
	}

	state := forwarderStateFrom(dto, req.State.InboxID)
	return infer.ReadResponse[InboxForwarderArgs, InboxForwarderState]{
		ID:     req.ID,
		Inputs: forwarderInputsFrom(state),
		State:  state,
	}, nil
}

// Update changes the five options in place with `PUT /forwarders/{id}`. Without this method infer's
// default diff replaces on every change (pulumi-go-provider infer/resource.go:1142).
func (f *InboxForwarder) Update(
	ctx context.Context, req infer.UpdateRequest[InboxForwarderArgs, InboxForwarderState],
) (infer.UpdateResponse[InboxForwarderState], error) {
	if req.DryRun {
		return infer.UpdateResponse[InboxForwarderState]{
			Output: applyForwarderInputs(req.State, req.Inputs),
		}, nil
	}

	client, err := f.client()
	if err != nil {
		return infer.UpdateResponse[InboxForwarderState]{}, err
	}

	dto, err := client.UpdateForwarder(ctx, req.ID, forwarderOptions(req.Inputs))
	if err != nil {
		return infer.UpdateResponse[InboxForwarderState]{}, err
	}

	return infer.UpdateResponse[InboxForwarderState]{
		Output: forwarderStateFrom(dto, req.Inputs.InboxID),
	}, nil
}

// Delete removes the forwarder. A forwarder that is already gone is a success, not a failure.
func (f *InboxForwarder) Delete(
	ctx context.Context, req infer.DeleteRequest[InboxForwarderState],
) (infer.DeleteResponse, error) {
	client, err := f.client()
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if err := client.DeleteForwarder(ctx, req.ID); err != nil && !IsGone(err) {
		return infer.DeleteResponse{}, err
	}
	return infer.DeleteResponse{}, nil
}
