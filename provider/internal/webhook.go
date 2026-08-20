package internal

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	_ "embed"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// WebhookEventName selects the MailSlurp event that triggers the webhook.
type WebhookEventName string

// The event names of the pinned API spec, in api/openapi.json.
const (
	WebhookEventEmailReceived        WebhookEventName = "EMAIL_RECEIVED"
	WebhookEventNewAITransformResult WebhookEventName = "NEW_AI_TRANSFORM_RESULT"
	WebhookEventNewEmail             WebhookEventName = "NEW_EMAIL"
	WebhookEventNewContact           WebhookEventName = "NEW_CONTACT"
	WebhookEventNewAttachment        WebhookEventName = "NEW_ATTACHMENT"
	WebhookEventEmailOpened          WebhookEventName = "EMAIL_OPENED"
	WebhookEventEmailRead            WebhookEventName = "EMAIL_READ"
	WebhookEventDeliveryStatus       WebhookEventName = "DELIVERY_STATUS"
	WebhookEventBounce               WebhookEventName = "BOUNCE"
	WebhookEventBounceRecipient      WebhookEventName = "BOUNCE_RECIPIENT"
	WebhookEventNewSMS               WebhookEventName = "NEW_SMS"
	WebhookEventNewGuestUser         WebhookEventName = "NEW_GUEST_USER"
)

// defaultWebhookEventName is the event the server subscribes a webhook to when the body names
// none. An omitted event name is not a clear: it resets the subscription to this value.
const defaultWebhookEventName = WebhookEventEmailReceived

// Values lists the allowed event names for the schema.
func (WebhookEventName) Values() []infer.EnumValue[WebhookEventName] {
	return []infer.EnumValue[WebhookEventName]{
		{Name: "EmailReceived", Value: WebhookEventEmailReceived,
			Description: "MailSlurp sends the event when an inbox receives an email payload."},
		{Name: "NewAiTransformResult", Value: WebhookEventNewAITransformResult,
			Description: "MailSlurp sends the event when an AI transformer produces a new extraction result."},
		{Name: "NewEmail", Value: WebhookEventNewEmail,
			Description: "MailSlurp sends the event when an inbox receives a new email."},
		{Name: "NewContact", Value: WebhookEventNewContact,
			Description: "MailSlurp sends the event when it creates a contact from inbound email activity."},
		{Name: "NewAttachment", Value: WebhookEventNewAttachment,
			Description: "MailSlurp sends the event when it detects an attachment on a received email."},
		{Name: "EmailOpened", Value: WebhookEventEmailOpened,
			Description: "MailSlurp sends the event when it tracks an email open."},
		{Name: "EmailRead", Value: WebhookEventEmailRead,
			Description: "MailSlurp sends the event when a recipient reads an email."},
		{Name: "DeliveryStatus", Value: WebhookEventDeliveryStatus,
			Description: "MailSlurp sends the event when the delivery status of a sent email changes."},
		{Name: "Bounce", Value: WebhookEventBounce,
			Description: "MailSlurp sends the event when an email bounces at the account level."},
		{Name: "BounceRecipient", Value: WebhookEventBounceRecipient,
			Description: "MailSlurp sends the event when an email address bounces."},
		{Name: "NewSms", Value: WebhookEventNewSMS,
			Description: "MailSlurp sends the event when a phone number receives an SMS message."},
		{Name: "NewGuestUser", Value: WebhookEventNewGuestUser,
			Description: "MailSlurp sends the event for a guest user."},
	}
}

// webhookExamples is the registry example page that make docs writes from docs/yaml.
//
//go:embed embed/webhook-examples.md
var webhookExamples string

// Webhook is the MailSlurp webhook resource.
type Webhook struct {
	config *Config
}

// Annotate provides the user-facing description of the Webhook resource.
func (w *Webhook) Annotate(a infer.Annotator) {
	a.Describe(&w, dedent(`
		A webhook that MailSlurp calls when an event happens. MailSlurp posts the
		event payload to the URL that you set.

		You can update every property of the webhook in place. Pulumi never
		replaces a webhook. A webhook that names no inbox listens to every inbox
		in the account. If you set the "inboxId" property, the webhook listens to
		that one inbox.

		This resource does not carry the "basicAuth" property of the API. The API
		reports the property as a boolean, so Pulumi cannot compare the user name
		and the password that it sent. The webhook list of the API also reports
		both values as plain text. Use the "includeHeaders" property to
		authenticate the request.
	`)+"\n\n"+webhookExamples)
}

// Schema property names of the Webhook diff contract.
const (
	webhookPropURL                           = "url"
	webhookPropName                          = "name"
	webhookPropEventName                     = "eventName"
	webhookPropIncludeHeaders                = "includeHeaders"
	webhookPropRequestBodyTemplate           = "requestBodyTemplate"
	webhookPropTags                          = "tags"
	webhookPropInboxID                       = "inboxId"
	webhookPropUseStaticIPRange              = "useStaticIpRange"
	webhookPropIgnoreInsecureSSLCertificates = "ignoreInsecureSslCertificates"
)

// WebhookArgs are the inputs of the Webhook resource.
type WebhookArgs struct {
	URL                           string            `pulumi:"url"`
	Name                          *string           `pulumi:"name,optional"`
	EventName                     *WebhookEventName `pulumi:"eventName,optional"`
	IncludeHeaders                map[string]string `pulumi:"includeHeaders,optional" provider:"secret"`
	RequestBodyTemplate           *string           `pulumi:"requestBodyTemplate,optional"`
	Tags                          []string          `pulumi:"tags,optional"`
	InboxID                       *string           `pulumi:"inboxId,optional"`
	UseStaticIPRange              *bool             `pulumi:"useStaticIpRange,optional"`
	IgnoreInsecureSSLCertificates *bool             `pulumi:"ignoreInsecureSslCertificates,optional"`
}

// Annotate provides the user-facing descriptions of the Webhook inputs.
func (wa *WebhookArgs) Annotate(a infer.Annotator) {
	a.Describe(&wa.URL, dedent(`
		The URL that MailSlurp posts the event payload to. Your server must
		accept the request. You can update this property in place.
	`))
	a.Describe(&wa.Name, dedent(`
		The display name of the webhook. You can update this property in place.
		Remove the property to clear the name.
	`))
	a.Describe(&wa.EventName, dedent(`
		The event that triggers the webhook. If you do not set this property,
		MailSlurp sends the "EMAIL_RECEIVED" event.
	`))
	a.Describe(&wa.IncludeHeaders, dedent(`
		The HTTP headers that MailSlurp sends with each webhook request. Each key
		is a header name, and each value is the header value. The provider marks
		the values as secret. MailSlurp reports the values as plain text, so
		anyone with the API key can read them.
	`))
	a.Describe(&wa.RequestBodyTemplate, dedent(`
		A template for the JSON body of the webhook request. Use "{{variableName}}"
		markers to insert values from the event payload.
	`))
	a.Describe(&wa.Tags, dedent(`
		The tags on the webhook. MailSlurp accepts the tags and never reports
		them, so Pulumi cannot compare this property. A change to the tags alone
		plans no update.
	`))
	a.Describe(&wa.InboxID, dedent(`
		The inbox that triggers the webhook. If you do not set this property,
		every inbox in the account triggers the webhook. You can move the webhook
		to another inbox in place. You cannot remove this property after you set
		it, because the API has no value for the account.
	`))
	a.Describe(&wa.UseStaticIPRange, dedent(`
		If you set this property, MailSlurp calls your server from its static IP
		range. The default value is false.
	`))
	a.Describe(&wa.IgnoreInsecureSSLCertificates, dedent(`
		If you set this property, MailSlurp accepts an insecure SSL certificate
		on your server. Use it for a self-signed certificate. The default value
		is false.
	`))
}

// WebhookState is the stored state of the Webhook resource.
type WebhookState struct {
	URL                           string            `pulumi:"url"`
	Name                          *string           `pulumi:"name,optional"`
	EventName                     WebhookEventName  `pulumi:"eventName"`
	IncludeHeaders                map[string]string `pulumi:"includeHeaders,optional" provider:"secret"`
	RequestBodyTemplate           *string           `pulumi:"requestBodyTemplate,optional"`
	InboxID                       *string           `pulumi:"inboxId,optional"`
	UseStaticIPRange              bool              `pulumi:"useStaticIpRange"`
	IgnoreInsecureSSLCertificates bool              `pulumi:"ignoreInsecureSslCertificates"`
	Method                        string            `pulumi:"method"`
	CreatedAt                     *string           `pulumi:"createdAt,optional"`
	HealthStatus                  *string           `pulumi:"healthStatus,optional"`
	Enabled                       *bool             `pulumi:"enabled,optional"`
}

// Annotate provides the user-facing descriptions of the Webhook outputs.
func (ws *WebhookState) Annotate(a infer.Annotator) {
	a.Describe(&ws.URL, "The URL that MailSlurp posts the event payload to.")
	a.Describe(&ws.Name, "The display name of the webhook.")
	a.Describe(&ws.EventName, "The event that triggers the webhook.")
	a.Describe(&ws.IncludeHeaders, dedent(`
		The HTTP headers that MailSlurp sends with each webhook request. The API
		names this value "requestHeaders" when it reports it.
	`))
	a.Describe(&ws.RequestBodyTemplate, "The template for the JSON body of the webhook request.")
	a.Describe(&ws.InboxID, dedent(`
		The inbox that triggers the webhook. An empty value means that every
		inbox in the account triggers the webhook.
	`))
	a.Describe(&ws.UseStaticIPRange, "MailSlurp reports whether it calls your server from its static IP range.")
	a.Describe(&ws.IgnoreInsecureSSLCertificates,
		"MailSlurp reports whether it accepts an insecure SSL certificate on your server.")
	a.Describe(&ws.Method, "The HTTP method that your server must accept. MailSlurp sends `POST`.")
	a.Describe(&ws.CreatedAt, "The time when MailSlurp created the webhook.")
	a.Describe(&ws.HealthStatus, dedent(`
		The health of the webhook, as MailSlurp computes it. The value is
		"HEALTHY" or "UNHEALTHY". The value changes often, so Pulumi never
		compares it.
	`))
	a.Describe(&ws.Enabled, dedent(`
		MailSlurp reports whether the webhook is enabled. The API does not
		document this property, and the value can be empty.
	`))
}

// The move rides a query parameter that names an inbox, and the API has no value that names the
// account. The client refuses a blank one, so the diff refuses the transition first.
const cannotLeaveInboxMessage = "The MailSlurp API cannot remove the webhook from its inbox. " +
	"Keep the `inboxId` property, or replace the webhook with `pulumi up --replace <urn>`."

// A webhook without an inbox must keep one unique pair of url and event name for the account, and
// the check reads the webhook itself as the duplicate. Probed live on 2026-08-18.
const cannotKeepURLAndEventMessage = "The MailSlurp API refuses an update that keeps the `url` and " +
	"the `eventName` of a webhook without an inbox. " +
	"Change the `url` or the `eventName`, or run `pulumi up --replace <urn>`."

func (w *Webhook) client() (Client, error) {
	if w.config == nil || w.config.client == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return nil, errors.New(notConfiguredMessage)
	}
	return w.config.client, nil
}

func boolOf(v *bool) bool { return v != nil && *v }

// wantedEvent answers the event the program asks for. An omitted event name is not a clear: the
// server resets the subscription to its own default, so the provider names that default.
func wantedEvent(v *WebhookEventName) WebhookEventName {
	if v == nil {
		return defaultWebhookEventName
	}
	return eventOrDefault(*v)
}

func eventOrDefault(v WebhookEventName) WebhookEventName {
	if v == "" {
		return defaultWebhookEventName
	}
	return v
}

// wantedHeaders copies the headers the program asks for. An empty map states no header, which is
// the same state as no map at all.
func wantedHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}

// headerList renders the schema map as the header object the API takes. The names sort, so one
// map always builds one body, and an empty map builds the empty list that clears the headers.
func headerList(in map[string]string) WebhookHeaders {
	list := make([]WebhookHeader, 0, len(in))
	for _, name := range slices.Sorted(maps.Keys(in)) {
		list = append(list, WebhookHeader{Name: name, Value: in[name]})
	}
	return WebhookHeaders{Headers: list}
}

// headersOf builds the write property of the options body. The API requires the list inside the
// object, so an empty map sends no header object at all.
func headersOf(in map[string]string) *WebhookHeaders {
	if len(in) == 0 {
		return nil
	}
	return ptr(headerList(in))
}

// headersFrom maps the read property requestHeaders onto the write property includeHeaders.
func headersFrom(headers *WebhookHeaders) map[string]string {
	if headers == nil || len(headers.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers.Headers))
	for _, header := range headers.Headers {
		out[header.Name] = header.Value
	}
	return out
}

// headersText renders the headers as one comparable string. The API states no order, so the
// names sort before the comparison.
func headersText(in map[string]string) *string {
	if len(in) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(in))
	for _, name := range slices.Sorted(maps.Keys(in)) {
		pairs = append(pairs, fmt.Sprintf("%q:%q", name, in[name]))
	}
	return ptr(strings.Join(pairs, ","))
}

// webhookProperty is one row of the Webhook diff contract. Both sides normalize to a
// string so one comparison serves every property type; a nil pointer means the property is unset.
type webhookProperty struct {
	name string
	// clearMessage refuses a transition the API cannot perform, and is empty when it can.
	clearMessage string
	prior        func(*WebhookState) *string
	desired      func(*WebhookArgs) *string
}

// webhookProperties pairs every diffed input with the way the diff reads it. Nothing on a
// webhook forces a replacement, and the tags never appear: the API never reports them.
func webhookProperties() []webhookProperty {
	flagText := func(v bool) *string { return ptr(strconv.FormatBool(v)) }

	return []webhookProperty{
		{name: webhookPropURL,
			prior:   func(s *WebhookState) *string { return text(&s.URL) },
			desired: func(a *WebhookArgs) *string { return text(&a.URL) }},
		{name: webhookPropName,
			prior:   func(s *WebhookState) *string { return text(s.Name) },
			desired: func(a *WebhookArgs) *string { return text(a.Name) }},
		{name: webhookPropEventName,
			prior:   func(s *WebhookState) *string { return ptr(string(eventOrDefault(s.EventName))) },
			desired: func(a *WebhookArgs) *string { return ptr(string(wantedEvent(a.EventName))) }},
		{name: webhookPropIncludeHeaders,
			prior:   func(s *WebhookState) *string { return headersText(s.IncludeHeaders) },
			desired: func(a *WebhookArgs) *string { return headersText(a.IncludeHeaders) }},
		{name: webhookPropRequestBodyTemplate,
			prior:   func(s *WebhookState) *string { return text(s.RequestBodyTemplate) },
			desired: func(a *WebhookArgs) *string { return text(a.RequestBodyTemplate) }},
		{name: webhookPropInboxID, clearMessage: cannotLeaveInboxMessage,
			prior:   func(s *WebhookState) *string { return text(s.InboxID) },
			desired: func(a *WebhookArgs) *string { return text(a.InboxID) }},
		{name: webhookPropUseStaticIPRange,
			prior:   func(s *WebhookState) *string { return flagText(s.UseStaticIPRange) },
			desired: func(a *WebhookArgs) *string { return flagText(boolOf(a.UseStaticIPRange)) }},
		{name: webhookPropIgnoreInsecureSSLCertificates,
			prior:   func(s *WebhookState) *string { return flagText(s.IgnoreInsecureSSLCertificates) },
			desired: func(a *WebhookArgs) *string { return flagText(boolOf(a.IgnoreInsecureSSLCertificates)) }},
	}
}

// diffKind answers the kind a change produces. No kind is ever a replacement, because the update
// covers every property, including the move to another inbox.
func (prop webhookProperty) diffKind(prior, desired *string) (p.DiffKind, error) {
	if desired == nil {
		if prop.clearMessage != "" {
			//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
			return "", errors.New(prop.clearMessage)
		}
		return p.Delete, nil
	}
	if prior == nil {
		return p.Add, nil
	}
	return p.Update, nil
}

// changedWebhookProperties answers the diffed properties the program changed.
func changedWebhookProperties(state WebhookState, in WebhookArgs) []string {
	var changed []string
	for _, prop := range webhookProperties() {
		if !equalText(prop.prior(&state), prop.desired(&in)) {
			changed = append(changed, prop.name)
		}
	}
	return changed
}

// headersOnlyChange reports whether the headers are the only change. The headers endpoint writes
// them alone, and it answers 200 whatever the url and the event name are.
func headersOnlyChange(state WebhookState, in WebhookArgs) bool {
	changed := changedWebhookProperties(state, in)
	return len(changed) == 1 && changed[0] == webhookPropIncludeHeaders
}

// refuseTheConflict guards the one update the API answers with 409. The server reads the webhook
// as a duplicate of the wanted state, so the update never converges and the message says so.
func refuseTheConflict(state WebhookState, in WebhookArgs, changed map[string]p.PropertyDiff) error {
	if len(changed) == 0 || headersOnlyChange(state, in) {
		return nil
	}
	// The duplicate check covers the account, so a webhook that keeps or takes an inbox is safe.
	if text(state.InboxID) != nil || text(in.InboxID) != nil {
		return nil
	}
	_, urlChanged := changed[webhookPropURL]
	_, eventChanged := changed[webhookPropEventName]
	if urlChanged || eventChanged {
		return nil
	}
	//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
	return errors.New(cannotKeepURLAndEventMessage)
}

// Check refuses a blank required property before the create reaches MailSlurp.
func (w *Webhook) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[WebhookArgs], error) {
	return checkRequiredInputs[WebhookArgs](ctx, req, webhookPropURL)
}

// Diff implements the Webhook contract. It emits every kind itself, because infer stops
// applying the `replaceOnChanges` tags as soon as a resource implements CustomDiff.
func (w *Webhook) Diff(
	_ context.Context, req infer.DiffRequest[WebhookArgs, WebhookState],
) (infer.DiffResponse, error) {
	state, inputs := req.State, req.Inputs
	detailed := map[string]p.PropertyDiff{}

	for _, prop := range webhookProperties() {
		prior, desired := prop.prior(&state), prop.desired(&inputs)
		if equalText(prior, desired) {
			continue
		}
		kind, err := prop.diffKind(prior, desired)
		if err != nil {
			return infer.DiffResponse{}, err
		}
		detailed[prop.name] = p.PropertyDiff{Kind: kind, InputDiff: true}
	}

	if err := refuseTheConflict(state, inputs, detailed); err != nil {
		return infer.DiffResponse{}, err
	}

	return infer.DiffResponse{HasChanges: len(detailed) > 0, DetailedDiff: detailed}, nil
}

// webhookOptions builds the whole options body. The update replaces the options rather than
// patching them, so every call carries the complete wanted state.
func webhookOptions(in WebhookArgs) WebhookOptions {
	opts := WebhookOptions{
		URL:                           in.URL,
		EventName:                     string(wantedEvent(in.EventName)),
		IncludeHeaders:                headersOf(in.IncludeHeaders),
		Tags:                          in.Tags,
		UseStaticIPRange:              ptr(boolOf(in.UseStaticIPRange)),
		IgnoreInsecureSSLCertificates: ptr(boolOf(in.IgnoreInsecureSSLCertificates)),
	}
	if name := text(in.Name); name != nil {
		opts.Name = *name
	}
	if template := text(in.RequestBodyTemplate); template != nil {
		opts.RequestBodyTemplate = *template
	}
	return opts
}

// webhookStateFrom maps an API response onto the stored state. The response carries every stored
// property, so no property travels from the prior state.
func webhookStateFrom(dto *WebhookDto) WebhookState {
	return WebhookState{
		URL:                           dto.URL,
		Name:                          text(dto.Name),
		EventName:                     eventOrDefault(WebhookEventName(dto.EventName)),
		IncludeHeaders:                headersFrom(dto.RequestHeaders),
		RequestBodyTemplate:           text(dto.RequestBodyTemplate),
		InboxID:                       text(dto.InboxID),
		UseStaticIPRange:              boolOf(dto.UseStaticIPRange),
		IgnoreInsecureSSLCertificates: boolOf(dto.IgnoreInsecureSSLCertificates),
		Method:                        dto.Method,
		CreatedAt:                     text(dto.CreatedAt),
		HealthStatus:                  text(dto.HealthStatus),
		Enabled:                       dto.Enabled,
	}
}

// webhookInputsFrom answers the inputs that produce the state, which an import has none of. The
// tags stay absent, because no answer of the API reports them.
func webhookInputsFrom(state WebhookState) WebhookArgs {
	return WebhookArgs{
		URL:                           state.URL,
		Name:                          state.Name,
		EventName:                     ptr(eventOrDefault(state.EventName)),
		IncludeHeaders:                wantedHeaders(state.IncludeHeaders),
		RequestBodyTemplate:           state.RequestBodyTemplate,
		InboxID:                       state.InboxID,
		UseStaticIPRange:              ptr(state.UseStaticIPRange),
		IgnoreInsecureSSLCertificates: ptr(state.IgnoreInsecureSSLCertificates),
	}
}

// applyWebhookInputs writes the wanted state onto a state value, for a preview that calls no API.
func applyWebhookInputs(state WebhookState, in WebhookArgs) WebhookState {
	state.URL = in.URL
	state.Name = text(in.Name)
	state.EventName = wantedEvent(in.EventName)
	state.IncludeHeaders = wantedHeaders(in.IncludeHeaders)
	state.RequestBodyTemplate = text(in.RequestBodyTemplate)
	state.InboxID = text(in.InboxID)
	state.UseStaticIPRange = boolOf(in.UseStaticIPRange)
	state.IgnoreInsecureSSLCertificates = boolOf(in.IgnoreInsecureSSLCertificates)
	return state
}

// wantedInbox answers the inbox every update names. The update replaces the whole state and the
// inbox rides the query, so a call that omits it moves the webhook back to the account.
func wantedInbox(in WebhookArgs) *string { return text(in.InboxID) }

// Create builds one webhook. A webhook that names an inbox listens to that inbox alone, and a
// webhook that names none listens to every inbox in the account and costs no inbox quota.
func (w *Webhook) Create(
	ctx context.Context, req infer.CreateRequest[WebhookArgs],
) (infer.CreateResponse[WebhookState], error) {
	if req.DryRun {
		return infer.CreateResponse[WebhookState]{
			Output: applyWebhookInputs(WebhookState{}, req.Inputs),
		}, nil
	}

	client, err := w.client()
	if err != nil {
		return infer.CreateResponse[WebhookState]{}, err
	}

	var dto *WebhookDto
	if inbox := text(req.Inputs.InboxID); inbox != nil {
		dto, err = client.CreateInboxWebhook(ctx, *inbox, webhookOptions(req.Inputs))
	} else {
		dto, err = client.CreateAccountWebhook(ctx, webhookOptions(req.Inputs))
	}
	if err != nil {
		return infer.CreateResponse[WebhookState]{}, err
	}

	return infer.CreateResponse[WebhookState]{ID: dto.ID, Output: webhookStateFrom(dto)}, nil
}

// Read refreshes from GET /webhooks/{webhookId}, and answers the inputs as well so `pulumi import`
// builds a complete resource. The list projection omits the headers, the body template and the method.
func (w *Webhook) Read(
	ctx context.Context, req infer.ReadRequest[WebhookArgs, WebhookState],
) (infer.ReadResponse[WebhookArgs, WebhookState], error) {
	client, err := w.client()
	if err != nil {
		return infer.ReadResponse[WebhookArgs, WebhookState]{}, err
	}

	dto, err := client.GetWebhook(ctx, req.ID)
	if err != nil {
		// A deleted webhook is drift. An empty response tells the engine the webhook is gone.
		if IsGone(err) {
			return infer.ReadResponse[WebhookArgs, WebhookState]{}, nil
		}
		return infer.ReadResponse[WebhookArgs, WebhookState]{}, err
	}

	state := webhookStateFrom(dto)
	return infer.ReadResponse[WebhookArgs, WebhookState]{
		ID:     req.ID,
		Inputs: webhookInputsFrom(state),
		State:  state,
	}, nil
}

// Update changes every property in place, and moves the webhook with the query parameter of the
// same call. It sends the whole wanted state, because the API destroys what the call omits.
func (w *Webhook) Update(
	ctx context.Context, req infer.UpdateRequest[WebhookArgs, WebhookState],
) (infer.UpdateResponse[WebhookState], error) {
	if req.DryRun {
		return infer.UpdateResponse[WebhookState]{Output: applyWebhookInputs(req.State, req.Inputs)}, nil
	}

	client, err := w.client()
	if err != nil {
		return infer.UpdateResponse[WebhookState]{}, err
	}

	var dto *WebhookDto
	if headersOnlyChange(req.State, req.Inputs) {
		// The headers endpoint writes the headers and disturbs nothing else. It is also
		// the only path an account webhook accepts while the url and the event name stay.
		dto, err = client.UpdateWebhookHeaders(ctx, req.ID, headerList(req.Inputs.IncludeHeaders))
	} else {
		dto, err = client.UpdateWebhook(ctx, req.ID,
			wantedInbox(req.Inputs), webhookOptions(req.Inputs))
	}
	if err != nil {
		return infer.UpdateResponse[WebhookState]{}, err
	}

	return infer.UpdateResponse[WebhookState]{Output: webhookStateFrom(dto)}, nil
}

// Delete removes the webhook. A webhook that is already gone is a success, not a failure.
func (w *Webhook) Delete(
	ctx context.Context, req infer.DeleteRequest[WebhookState],
) (infer.DeleteResponse, error) {
	client, err := w.client()
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if err := client.DeleteWebhook(ctx, req.ID); err != nil && !IsGone(err) {
		return infer.DeleteResponse{}, err
	}
	return infer.DeleteResponse{}, nil
}
