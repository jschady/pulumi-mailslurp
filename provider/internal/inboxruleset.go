package internal

import (
	"context"
	"errors"

	_ "embed"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// RulesetScope selects the activity that MailSlurp applies the ruleset to.
type RulesetScope string

// The scope values of the pinned API spec, in api/openapi.json.
const (
	RulesetScopeReceivingEmails RulesetScope = "RECEIVING_EMAILS"
	RulesetScopeSendingEmails   RulesetScope = "SENDING_EMAILS"
	RulesetScopeReceivingSMS    RulesetScope = "RECEIVING_SMS"
	RulesetScopeSendingSMS      RulesetScope = "SENDING_SMS"
)

// Values lists the allowed scopes for the schema.
func (RulesetScope) Values() []infer.EnumValue[RulesetScope] {
	return []infer.EnumValue[RulesetScope]{
		{Name: "ReceivingEmails", Value: RulesetScopeReceivingEmails,
			Description: "MailSlurp applies the ruleset to every email that the inbox receives."},
		{Name: "SendingEmails", Value: RulesetScopeSendingEmails,
			Description: "MailSlurp applies the ruleset to every email that the inbox sends."},
		{Name: "ReceivingSms", Value: RulesetScopeReceivingSMS,
			Description: "MailSlurp applies the ruleset to every SMS message that a phone number receives."},
		{Name: "SendingSms", Value: RulesetScopeSendingSMS,
			Description: "MailSlurp applies the ruleset to every SMS message that a phone number sends."},
	}
}

// RulesetAction selects what MailSlurp does when the target matches.
type RulesetAction string

// The action values of the pinned API spec, in api/openapi.json.
const (
	RulesetActionBlock        RulesetAction = "BLOCK"
	RulesetActionAllow        RulesetAction = "ALLOW"
	RulesetActionFilterRemove RulesetAction = "FILTER_REMOVE"
	RulesetActionBounceSoft   RulesetAction = "BOUNCE_SOFT"
	RulesetActionBounceHard   RulesetAction = "BOUNCE_HARD"
)

// Values lists the allowed actions for the schema.
func (RulesetAction) Values() []infer.EnumValue[RulesetAction] {
	return []infer.EnumValue[RulesetAction]{
		{Name: "Block", Value: RulesetActionBlock,
			Description: "MailSlurp stops the email when the target matches."},
		{Name: "Allow", Value: RulesetActionAllow,
			Description: "MailSlurp permits the email when the target matches, and this action wins over a block."},
		{Name: "FilterRemove", Value: RulesetActionFilterRemove,
			Description: "MailSlurp removes the matched address from the email, and the rest of the email continues."},
		{Name: "BounceSoft", Value: RulesetActionBounceSoft,
			Description: "MailSlurp answers the sender with a soft bounce, which states a temporary failure."},
		{Name: "BounceHard", Value: RulesetActionBounceHard,
			Description: "MailSlurp answers the sender with a hard bounce, which states a permanent failure."},
	}
}

// rulesetHandlerException is the only handler value the API reports. It stays a plain string in the
// schema, so a value MailSlurp adds later reaches a stack instead of breaking it.
const rulesetHandlerException = "EXCEPTION"

// Schema property names of the InboxRuleset diff contract.
const (
	rulesetPropInboxID   = "inboxId"
	rulesetPropScope     = "scope"
	rulesetPropAction    = "action"
	rulesetPropTarget    = "target"
	rulesetPropHandler   = "handler"
	rulesetPropCreatedAt = "createdAt"
)

// inboxRulesetExamples is the registry example page that make docs writes from docs/yaml.
//
//go:embed embed/inboxruleset-examples.md
var inboxRulesetExamples string

// InboxRuleset is the MailSlurp inbox ruleset resource.
type InboxRuleset struct {
	config *Config
}

// Annotate provides the user-facing description of the InboxRuleset resource.
func (r *InboxRuleset) Annotate(a infer.Annotator) {
	a.Describe(&r, dedent(`
		An inbox ruleset that blocks or allows email for one inbox. MailSlurp
		applies the ruleset when the inbox sends or receives email.

		You cannot update a ruleset. The API offers no update call, so Pulumi
		replaces the ruleset when you change any property. Pulumi creates the new
		ruleset, and then it deletes the old one. A ruleset is free, and it never
		counts against the inbox creation quota.
	`)+"\n\n"+inboxRulesetExamples)
}

const rulesetReplaceWarning = "**Warning:** If you change this property, Pulumi replaces the " +
	"ruleset. The new ruleset carries a new identifier."

// InboxRulesetArgs are the inputs of the InboxRuleset resource. The API requires every one, and a
// change to any of them replaces the ruleset.
type InboxRulesetArgs struct {
	InboxID string        `pulumi:"inboxId" provider:"replaceOnChanges"`
	Scope   RulesetScope  `pulumi:"scope" provider:"replaceOnChanges"`
	Action  RulesetAction `pulumi:"action" provider:"replaceOnChanges"`
	Target  string        `pulumi:"target" provider:"replaceOnChanges"`
}

// Annotate provides the user-facing descriptions of the InboxRuleset inputs.
func (ra *InboxRulesetArgs) Annotate(a infer.Annotator) {
	a.Describe(&ra.InboxID, dedent(`
		The inbox that the ruleset applies to. MailSlurp matches the email of the
		inbox against the target. `+rulesetReplaceWarning+`
	`))
	a.Describe(&ra.Scope, dedent(`
		The activity that the ruleset applies to. Use "RECEIVING_EMAILS" for the
		email the inbox receives. Use "SENDING_EMAILS" for the email the inbox
		sends. `+rulesetReplaceWarning+`
	`))
	a.Describe(&ra.Action, dedent(`
		The action that MailSlurp takes when the target matches. The "ALLOW"
		action wins over the "BLOCK" action. The "FILTER_REMOVE" action removes
		the matched address and keeps the rest of the email. `+rulesetReplaceWarning+`
	`))
	a.Describe(&ra.Target, dedent(`
		The email address or the pattern that MailSlurp matches. Use
		"*@example.com" to match every address of a domain. Use one full address
		to match that address alone. `+rulesetReplaceWarning+`
	`))
}

// InboxRulesetState is the stored state of the InboxRuleset resource.
type InboxRulesetState struct {
	InboxID   string        `pulumi:"inboxId"`
	Scope     RulesetScope  `pulumi:"scope"`
	Action    RulesetAction `pulumi:"action"`
	Target    string        `pulumi:"target"`
	Handler   string        `pulumi:"handler"`
	CreatedAt string        `pulumi:"createdAt"`
	PhoneID   *string       `pulumi:"phoneId,optional"`
}

// Annotate provides the user-facing descriptions of the InboxRuleset outputs.
func (rs *InboxRulesetState) Annotate(a infer.Annotator) {
	a.Describe(&rs.InboxID, "The inbox that the ruleset applies to.")
	a.Describe(&rs.Scope, "The activity that the ruleset applies to.")
	a.Describe(&rs.Action, "The action that MailSlurp takes when the target matches.")
	a.Describe(&rs.Target, "The email address or the pattern that MailSlurp matches.")
	a.Describe(&rs.Handler, "The handler that MailSlurp runs for the ruleset. The API reports one value, `EXCEPTION`.")
	a.Describe(&rs.CreatedAt, "The time when MailSlurp created the ruleset.")
	a.Describe(&rs.PhoneID, dedent(`
		The phone number that the ruleset applies to. This resource always names
		an inbox, so MailSlurp reports no phone number.
	`))
}

func (r *InboxRuleset) client() (Client, error) {
	if r.config == nil || r.config.client == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return nil, errors.New(notConfiguredMessage)
	}
	return r.config.client, nil
}

// rulesetStateFrom maps an API response onto the stored state. The API marks `inboxId` nullable, so
// the inbox the caller named stands when the answer omits it: a blank input plans a replacement.
func rulesetStateFrom(dto *RulesetDto, inboxID string) InboxRulesetState {
	state := InboxRulesetState{
		InboxID:   inboxID,
		Scope:     RulesetScope(dto.Scope),
		Action:    RulesetAction(dto.Action),
		Target:    dto.Target,
		Handler:   dto.Handler,
		CreatedAt: dto.CreatedAt,
		PhoneID:   text(dto.PhoneID),
	}
	if reported := text(dto.InboxID); reported != nil {
		state.InboxID = *reported
	}
	return state
}

// rulesetInputsFrom answers the inputs that produce the state. An import starts with no input, so
// the refreshed state is the only source of them.
func rulesetInputsFrom(state InboxRulesetState) InboxRulesetArgs {
	return InboxRulesetArgs{
		InboxID: state.InboxID,
		Scope:   state.Scope,
		Action:  state.Action,
		Target:  state.Target,
	}
}

// stateFromRulesetInputs writes the wanted state for a preview that calls no API.
func stateFromRulesetInputs(in InboxRulesetArgs) InboxRulesetState {
	return InboxRulesetState{
		InboxID: in.InboxID,
		Scope:   in.Scope,
		Action:  in.Action,
		Target:  in.Target,
	}
}

// Check refuses a blank required property before the create reaches MailSlurp.
func (r *InboxRuleset) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[InboxRulesetArgs], error) {
	return checkRequiredInputs[InboxRulesetArgs](ctx, req,
		rulesetPropInboxID, rulesetPropScope, rulesetPropAction, rulesetPropTarget)
}

// Create builds one ruleset for one inbox with `POST /rulesets?inboxId=`. The inbox always travels:
// a create that names none builds a ruleset for the whole account, which this resource never wants.
func (r *InboxRuleset) Create(
	ctx context.Context, req infer.CreateRequest[InboxRulesetArgs],
) (infer.CreateResponse[InboxRulesetState], error) {
	if req.DryRun {
		return infer.CreateResponse[InboxRulesetState]{
			Output: stateFromRulesetInputs(req.Inputs),
		}, nil
	}

	client, err := r.client()
	if err != nil {
		return infer.CreateResponse[InboxRulesetState]{}, err
	}

	dto, err := client.CreateRuleset(ctx, req.Inputs.InboxID, RulesetOptions{
		Scope:  string(req.Inputs.Scope),
		Action: string(req.Inputs.Action),
		Target: req.Inputs.Target,
	})
	if err != nil {
		return infer.CreateResponse[InboxRulesetState]{}, err
	}

	return infer.CreateResponse[InboxRulesetState]{
		ID:     dto.ID,
		Output: rulesetStateFrom(dto, req.Inputs.InboxID),
	}, nil
}

// Read refreshes from `GET /rulesets/{id}`. It answers the inputs as well, so a refresh reports
// drift on every property and `pulumi import` builds a complete resource.
func (r *InboxRuleset) Read(
	ctx context.Context, req infer.ReadRequest[InboxRulesetArgs, InboxRulesetState],
) (infer.ReadResponse[InboxRulesetArgs, InboxRulesetState], error) {
	client, err := r.client()
	if err != nil {
		return infer.ReadResponse[InboxRulesetArgs, InboxRulesetState]{}, err
	}

	dto, err := client.GetRuleset(ctx, req.ID)
	if err != nil {
		// A deleted ruleset is drift. An empty response tells the engine the ruleset is gone.
		if IsGone(err) {
			return infer.ReadResponse[InboxRulesetArgs, InboxRulesetState]{}, nil
		}
		return infer.ReadResponse[InboxRulesetArgs, InboxRulesetState]{}, err
	}

	state := rulesetStateFrom(dto, req.State.InboxID)
	return infer.ReadResponse[InboxRulesetArgs, InboxRulesetState]{
		ID:     req.ID,
		Inputs: rulesetInputsFrom(state),
		State:  state,
	}, nil
}

// Delete removes the ruleset. A ruleset that is already gone is a success, not a failure.
func (r *InboxRuleset) Delete(
	ctx context.Context, req infer.DeleteRequest[InboxRulesetState],
) (infer.DeleteResponse, error) {
	client, err := r.client()
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if err := client.DeleteRuleset(ctx, req.ID); err != nil && !IsGone(err) {
		return infer.DeleteResponse{}, err
	}
	return infer.DeleteResponse{}, nil
}
