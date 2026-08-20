package internal

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	_ "embed"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// InboxType selects how MailSlurp stores and receives email for an inbox.
type InboxType string

const (
	// InboxTypeHTTP stores email with HTTP endpoints and webhooks.
	InboxTypeHTTP InboxType = "HTTP_INBOX"
	// InboxTypeSMTP receives email on a real SMTP mail server.
	InboxTypeSMTP InboxType = "SMTP_INBOX"
)

// Values lists the allowed inbox types for the schema.
func (InboxType) Values() []infer.EnumValue[InboxType] {
	return []infer.EnumValue[InboxType]{
		{
			Name:        "HttpInbox",
			Value:       InboxTypeHTTP,
			Description: "The inbox stores email with HTTP endpoints. Use this type for tests and webhooks.",
		},
		{
			Name:        "SmtpInbox",
			Value:       InboxTypeSMTP,
			Description: "The inbox receives email on a real SMTP mail server. Use this type for external senders.",
		},
	}
}

// inboxExamples is the registry example page that make docs writes from docs/yaml.
//
//go:embed embed/inbox-examples.md
var inboxExamples string

// Inbox is the MailSlurp inbox resource.
type Inbox struct {
	config *Config
}

// Annotate provides the user-facing description of the Inbox resource.
func (i *Inbox) Annotate(a infer.Annotator) {
	a.Describe(&i, dedent(`
		An email inbox on MailSlurp. The inbox has an email address that can
		send and receive email.

		You can update the "name", "description", "tags", "expiresAt", and
		"favourite" properties in place. If you change any other property,
		Pulumi replaces the inbox. Each replacement creates a new inbox, which
		counts against the 30-day inbox creation quota of your MailSlurp
		account. MailSlurp bills each creation on paid plans. If you remove a
		property that forces a replacement, Pulumi keeps the inbox.
	`)+"\n\n"+inboxExamples)
}

const inboxQuotaWarning = `**Warning:** If you change this property, Pulumi replaces the inbox. ` +
	`The new inbox counts against the 30-day creation quota, and MailSlurp bills it.`

// InboxArgs are the inputs of the Inbox resource.
type InboxArgs struct {
	Name            *string    `pulumi:"name,optional"`
	Description     *string    `pulumi:"description,optional"`
	Tags            []string   `pulumi:"tags,optional"`
	ExpiresAt       *string    `pulumi:"expiresAt,optional"`
	Favourite       *bool      `pulumi:"favourite,optional"`
	EmailAddress    *string    `pulumi:"emailAddress,optional" provider:"replaceOnChanges"`
	DomainName      *string    `pulumi:"domainName,optional" provider:"replaceOnChanges"`
	DomainID        *string    `pulumi:"domainId,optional" provider:"replaceOnChanges"`
	InboxType       *InboxType `pulumi:"inboxType,optional" provider:"replaceOnChanges"`
	UseDomainPool   *bool      `pulumi:"useDomainPool,optional" provider:"replaceOnChanges"`
	UseShortAddress *bool      `pulumi:"useShortAddress,optional" provider:"replaceOnChanges"`
	VirtualInbox    *bool      `pulumi:"virtualInbox,optional" provider:"replaceOnChanges"`
	Prefix          *string    `pulumi:"prefix,optional" provider:"replaceOnChanges"`
	ExpiresIn       *int64     `pulumi:"expiresIn,optional" provider:"replaceOnChanges"`
}

// Annotate provides the user-facing descriptions of the Inbox inputs.
func (ia *InboxArgs) Annotate(a infer.Annotator) {
	a.Describe(&ia.Name, dedent(`
		The display name of the inbox. You can update this property in place.
		The API cannot clear this property after you set it.
	`))
	a.Describe(&ia.Description, dedent(`
		A free-text description of the inbox. You can update this property in
		place. The API cannot clear this property after you set it.
	`))
	a.Describe(&ia.Tags, dedent(`
		The tags on the inbox. Use tags to group and find inboxes. You can
		update this property in place. Set an empty list to clear the tags.
	`))
	a.Describe(&ia.ExpiresAt, dedent(`
		The time when MailSlurp expires the inbox, in RFC 3339 format. You can
		update this property in place. The API cannot clear this property after
		you set it.
	`))
	a.Describe(&ia.Favourite, dedent(`
		If you set this property, MailSlurp marks the inbox as a favourite in
		the dashboard. You can update this property in place.
	`))
	a.Describe(&ia.EmailAddress, dedent(`
		The full email address of the inbox. When you do not set this
		property, MailSlurp assigns an address. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.DomainName, dedent(`
		The custom domain that provides the inbox address. The domain must be
		verified in your MailSlurp account. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.DomainID, dedent(`
		The identifier of a verified custom domain that provides the inbox
		address. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.InboxType, dedent(`
		The inbox type: "HTTP_INBOX" or "SMTP_INBOX". A refresh keeps the
		prior value when the API does not report a type. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.UseDomainPool, dedent(`
		If you set this property, MailSlurp assigns the address from the shared
		domain pool. The API does not echo this property, so Pulumi keeps it in
		the state and does not diff it against the API. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.UseShortAddress, dedent(`
		If you set this property, MailSlurp assigns a short email address to the
		inbox. The API does not echo this property, so Pulumi keeps it in the
		state and does not diff it against the API. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.VirtualInbox, dedent(`
		If you set this property, MailSlurp creates a virtual inbox. A virtual
		inbox receives email but never sends it to real recipients. Use it for
		safe tests. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.Prefix, dedent(`
		A prefix for the local part of the assigned email address. The API
		does not echo this property, so Pulumi keeps it in the state and does
		not diff it against the API. `+inboxQuotaWarning+`
	`))
	a.Describe(&ia.ExpiresIn, dedent(`
		The lifetime of the inbox in milliseconds. The API does not echo this
		property, so Pulumi keeps it in the state and does not diff it against
		the API. `+inboxQuotaWarning+`
	`))
}

// InboxState is the stored state of the Inbox resource.
type InboxState struct {
	Name            *string    `pulumi:"name,optional"`
	Description     *string    `pulumi:"description,optional"`
	Tags            []string   `pulumi:"tags,optional"`
	ExpiresAt       *string    `pulumi:"expiresAt,optional"`
	Favourite       *bool      `pulumi:"favourite,optional"`
	EmailAddress    string     `pulumi:"emailAddress"`
	DomainName      *string    `pulumi:"domainName,optional"`
	DomainID        *string    `pulumi:"domainId,optional"`
	InboxType       *InboxType `pulumi:"inboxType,optional"`
	UseDomainPool   *bool      `pulumi:"useDomainPool,optional"`
	UseShortAddress *bool      `pulumi:"useShortAddress,optional"`
	VirtualInbox    *bool      `pulumi:"virtualInbox,optional"`
	Prefix          *string    `pulumi:"prefix,optional"`
	ExpiresIn       *int64     `pulumi:"expiresIn,optional"`
	CreatedAt       string     `pulumi:"createdAt"`
	ReadOnly        bool       `pulumi:"readOnly"`
	FunctionsAs     *string    `pulumi:"functionsAs,optional"`
	LocalPart       *string    `pulumi:"localPart,optional"`
	Domain          *string    `pulumi:"domain,optional"`
	AccountRegion   *string    `pulumi:"accountRegion,optional"`
}

// Annotate provides the user-facing descriptions of the Inbox outputs.
func (is *InboxState) Annotate(a infer.Annotator) {
	a.Describe(&is.Name, "The display name of the inbox.")
	a.Describe(&is.Description, "The free-text description of the inbox.")
	a.Describe(&is.Tags, "The tags on the inbox.")
	a.Describe(&is.ExpiresAt, "The time when MailSlurp expires the inbox, in RFC 3339 format.")
	a.Describe(&is.Favourite, "MailSlurp reports whether the inbox is a favourite in the dashboard.")
	a.Describe(&is.EmailAddress, "The full email address of the inbox.")
	a.Describe(&is.DomainName, "The custom domain that provides the inbox address.")
	a.Describe(&is.DomainID, "The identifier of the custom domain that provides the inbox address.")
	a.Describe(&is.InboxType, "The inbox type.")
	a.Describe(&is.UseDomainPool,
		"The shared MailSlurp domain pool value of the inbox. The API does not echo this property.")
	a.Describe(&is.UseShortAddress,
		"The short address value of the inbox. The API does not echo this property.")
	a.Describe(&is.VirtualInbox,
		"MailSlurp reports whether the inbox is virtual. A virtual inbox never sends email to real recipients.")
	a.Describe(&is.Prefix,
		"The prefix for the local part of the assigned email address. The API does not echo this property.")
	a.Describe(&is.ExpiresIn, "The lifetime of the inbox in milliseconds. The API does not echo this property.")
	a.Describe(&is.CreatedAt, "The time when MailSlurp created the inbox.")
	a.Describe(&is.ReadOnly, "MailSlurp reports whether the inbox is read-only.")
	a.Describe(&is.FunctionsAs, "The special function of the inbox, for example an alias or a catch-all.")
	a.Describe(&is.LocalPart, "The local part of the inbox email address.")
	a.Describe(&is.Domain, "The domain part of the inbox email address.")
	a.Describe(&is.AccountRegion, "The region of the account that holds the inbox.")
}

const (
	notConfiguredMessage = "The MailSlurp provider is not configured. Set the API key and try again."

	// PATCH swallows an explicit null and answers 200, so this transition never converges.
	cannotClearMessage = "The MailSlurp API cannot clear the `%s` property of the inbox. " +
		"Keep a value, or replace the inbox with `pulumi up --replace <urn>`."
)

// Schema property names of the Inbox diff contract.
const (
	inboxPropName            = "name"
	inboxPropDescription     = "description"
	inboxPropTags            = "tags"
	inboxPropExpiresAt       = "expiresAt"
	inboxPropFavourite       = "favourite"
	inboxPropEmailAddress    = "emailAddress"
	inboxPropDomainName      = "domainName"
	inboxPropDomainID        = "domainId"
	inboxPropInboxType       = "inboxType"
	inboxPropUseDomainPool   = "useDomainPool"
	inboxPropUseShortAddress = "useShortAddress"
	inboxPropVirtualInbox    = "virtualInbox"
	inboxPropPrefix          = "prefix"
	inboxPropExpiresIn       = "expiresIn"
)

func ptr[T any](v T) *T { return &v }

// text reads an optional string property. The live API answers the empty string for an inbox
// that carries no name, so the empty string and the absent property mean the same thing here.
func text(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	return v
}

func tagsOrEmpty(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func (i *Inbox) client() (Client, error) {
	if i.config == nil || i.config.client == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return nil, errors.New(notConfiguredMessage)
	}
	return i.config.client, nil
}

// inboxProperty is one row of the diff contract. Both sides normalize to a string so
// one comparison serves every property type; a nil pointer means the property is unset.
type inboxProperty struct {
	name    string
	replace bool
	prior   func(*InboxState) *string
	desired func(*InboxArgs) *string
}

// inboxProperties pairs every input property with the diff kind its change produces. It is the
// sole source of the nine replacements: infer drops the `replaceOnChanges` tags under CustomDiff.
func inboxProperties() []inboxProperty {
	boolText := func(v *bool) *string {
		if v == nil {
			return nil
		}
		return ptr(strconv.FormatBool(*v))
	}
	// An unset list and an unset flag carry a value the API can represent, so they never reach
	// the clear guard below.
	listText := func(v []string) *string { return ptr(fmt.Sprintf("%q", tagsOrEmpty(v))) }
	flagText := func(v *bool) *string { return ptr(strconv.FormatBool(v != nil && *v)) }

	return []inboxProperty{
		// Update in place through PATCH /inboxes/{inboxId}.
		{name: inboxPropName,
			prior:   func(s *InboxState) *string { return text(s.Name) },
			desired: func(a *InboxArgs) *string { return text(a.Name) }},
		{name: inboxPropDescription,
			prior:   func(s *InboxState) *string { return text(s.Description) },
			desired: func(a *InboxArgs) *string { return text(a.Description) }},
		{name: inboxPropTags,
			prior:   func(s *InboxState) *string { return listText(s.Tags) },
			desired: func(a *InboxArgs) *string { return listText(a.Tags) }},
		{name: inboxPropExpiresAt,
			prior:   func(s *InboxState) *string { return text(s.ExpiresAt) },
			desired: func(a *InboxArgs) *string { return text(a.ExpiresAt) }},
		{name: inboxPropFavourite,
			prior:   func(s *InboxState) *string { return flagText(s.Favourite) },
			desired: func(a *InboxArgs) *string { return flagText(a.Favourite) }},

		// Create-only. Every change here destroys the inbox and bills a new one.
		{name: inboxPropEmailAddress, replace: true,
			prior:   func(s *InboxState) *string { return text(&s.EmailAddress) },
			desired: func(a *InboxArgs) *string { return text(a.EmailAddress) }},
		{name: inboxPropDomainName, replace: true,
			prior:   func(s *InboxState) *string { return text(s.DomainName) },
			desired: func(a *InboxArgs) *string { return text(a.DomainName) }},
		{name: inboxPropDomainID, replace: true,
			prior:   func(s *InboxState) *string { return text(s.DomainID) },
			desired: func(a *InboxArgs) *string { return text(a.DomainID) }},
		{name: inboxPropInboxType, replace: true,
			prior:   func(s *InboxState) *string { return inboxTypeText(s.InboxType) },
			desired: func(a *InboxArgs) *string { return inboxTypeText(a.InboxType) }},
		{name: inboxPropUseDomainPool, replace: true,
			prior:   func(s *InboxState) *string { return boolText(s.UseDomainPool) },
			desired: func(a *InboxArgs) *string { return boolText(a.UseDomainPool) }},
		{name: inboxPropUseShortAddress, replace: true,
			prior:   func(s *InboxState) *string { return boolText(s.UseShortAddress) },
			desired: func(a *InboxArgs) *string { return boolText(a.UseShortAddress) }},
		{name: inboxPropVirtualInbox, replace: true,
			prior:   func(s *InboxState) *string { return boolText(s.VirtualInbox) },
			desired: func(a *InboxArgs) *string { return boolText(a.VirtualInbox) }},
		{name: inboxPropPrefix, replace: true,
			prior:   func(s *InboxState) *string { return text(s.Prefix) },
			desired: func(a *InboxArgs) *string { return text(a.Prefix) }},
		{name: inboxPropExpiresIn, replace: true,
			prior:   func(s *InboxState) *string { return numberText(s.ExpiresIn) },
			desired: func(a *InboxArgs) *string { return numberText(a.ExpiresIn) }},
	}
}

func inboxTypeText(v *InboxType) *string {
	if v == nil || *v == "" {
		return nil
	}
	return ptr(string(*v))
}

func numberText(v *int64) *string {
	if v == nil {
		return nil
	}
	return ptr(strconv.FormatInt(*v, 10))
}

// diffKind answers the kind a change produces, and false when the change is not a diff at all.
func (prop inboxProperty) diffKind(prior, desired *string) (p.DiffKind, bool, error) {
	if prop.replace {
		// An unset create-only input states no opinion about the property. Reading it as a
		// removal would destroy the inbox, lose every stored email, and bill a new inbox.
		if desired == nil {
			return "", false, nil
		}
		if prior == nil {
			return p.AddReplace, true, nil
		}
		return p.UpdateReplace, true, nil
	}
	if desired == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return "", false, fmt.Errorf(cannotClearMessage, prop.name)
	}
	if prior == nil {
		return p.Add, true, nil
	}
	return p.Update, true, nil
}

// Diff implements the Inbox contract. It emits every replacement itself, because infer
// stops applying the `replaceOnChanges` tags as soon as a resource implements CustomDiff.
func (i *Inbox) Diff(
	_ context.Context, req infer.DiffRequest[InboxArgs, InboxState],
) (infer.DiffResponse, error) {
	state, inputs := req.State, req.Inputs
	detailed := map[string]p.PropertyDiff{}

	for _, prop := range inboxProperties() {
		prior, desired := prop.prior(&state), prop.desired(&inputs)
		if equalText(prior, desired) {
			continue
		}
		kind, changed, err := prop.diffKind(prior, desired)
		if err != nil {
			return infer.DiffResponse{}, err
		}
		if !changed {
			continue
		}
		detailed[prop.name] = p.PropertyDiff{Kind: kind, InputDiff: true}
	}

	return infer.DiffResponse{HasChanges: len(detailed) > 0, DetailedDiff: detailed}, nil
}

func equalText(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// carried holds the seven properties the API never echoes in the shape the program wrote them.
// A refresh keeps them, because comparing them against the API would loop the diff forever.
func carried(in InboxArgs) InboxState {
	return InboxState{
		DomainName:      text(in.DomainName),
		DomainID:        text(in.DomainID),
		InboxType:       in.InboxType,
		UseDomainPool:   in.UseDomainPool,
		UseShortAddress: in.UseShortAddress,
		Prefix:          text(in.Prefix),
		ExpiresIn:       in.ExpiresIn,
	}
}

// stateFrom maps an API response onto the stored state. The prior state supplies the properties
// the response cannot carry.
func stateFrom(dto *InboxDto, prior InboxState) InboxState {
	state := InboxState{
		Name:          text(dto.Name),
		Description:   text(dto.Description),
		Tags:          tagsOrEmpty(dto.Tags),
		ExpiresAt:     text(dto.ExpiresAt),
		Favourite:     ptr(dto.Favourite),
		EmailAddress:  prior.EmailAddress,
		CreatedAt:     prior.CreatedAt,
		ReadOnly:      dto.ReadOnly,
		VirtualInbox:  ptr(dto.VirtualInbox),
		FunctionsAs:   text(dto.FunctionsAs),
		LocalPart:     text(dto.LocalPart),
		Domain:        text(dto.Domain),
		AccountRegion: text(dto.AccountRegion),

		DomainName:      prior.DomainName,
		DomainID:        prior.DomainID,
		UseDomainPool:   prior.UseDomainPool,
		UseShortAddress: prior.UseShortAddress,
		Prefix:          prior.Prefix,
		ExpiresIn:       prior.ExpiresIn,
	}

	// Both outputs are required, so a response that omits one must not blank the state.
	if dto.EmailAddress != "" {
		state.EmailAddress = dto.EmailAddress
	}
	if dto.CreatedAt != "" {
		state.CreatedAt = dto.CreatedAt
	}

	// The API answers a null inboxType for some inboxes. A diff against that null would propose
	// a replacement on every update, so the prior value stands.
	if reported := text(dto.InboxType); reported != nil {
		state.InboxType = ptr(InboxType(*reported))
	} else {
		state.InboxType = prior.InboxType
	}

	return state
}

// inboxInputsFrom answers the inputs that produce the state. An import starts with no input, so
// the refreshed state is the only source of them.
func inboxInputsFrom(state InboxState) InboxArgs {
	return InboxArgs{
		Name:            state.Name,
		Description:     state.Description,
		Tags:            state.Tags,
		ExpiresAt:       state.ExpiresAt,
		Favourite:       state.Favourite,
		EmailAddress:    text(&state.EmailAddress),
		DomainName:      state.DomainName,
		DomainID:        state.DomainID,
		InboxType:       state.InboxType,
		UseDomainPool:   state.UseDomainPool,
		UseShortAddress: state.UseShortAddress,
		VirtualInbox:    state.VirtualInbox,
		Prefix:          state.Prefix,
		ExpiresIn:       state.ExpiresIn,
	}
}

func createOptions(in InboxArgs) CreateInboxOptions {
	return CreateInboxOptions{
		Name:            text(in.Name),
		Description:     text(in.Description),
		Tags:            in.Tags,
		EmailAddress:    text(in.EmailAddress),
		DomainName:      text(in.DomainName),
		DomainID:        text(in.DomainID),
		ExpiresAt:       text(in.ExpiresAt),
		ExpiresIn:       in.ExpiresIn,
		Favourite:       in.Favourite,
		InboxType:       inboxTypeText(in.InboxType),
		Prefix:          text(in.Prefix),
		UseDomainPool:   in.UseDomainPool,
		UseShortAddress: in.UseShortAddress,
		VirtualInbox:    in.VirtualInbox,
	}
}

// updateOptions sends all five updatable properties as full desired state. A nil
// string stays absent from the body: an explicit null returns 200 and changes nothing.
func updateOptions(in InboxArgs) UpdateInboxOptions {
	return UpdateInboxOptions{
		Name:        text(in.Name),
		Description: text(in.Description),
		Tags:        ptr(tagsOrEmpty(in.Tags)),
		ExpiresAt:   text(in.ExpiresAt),
		Favourite:   ptr(in.Favourite != nil && *in.Favourite),
	}
}

// applyInputs writes the five updatable properties of the desired state onto a state value.
func applyInputs(state InboxState, in InboxArgs) InboxState {
	state.Name = text(in.Name)
	state.Description = text(in.Description)
	state.Tags = tagsOrEmpty(in.Tags)
	state.ExpiresAt = text(in.ExpiresAt)
	state.Favourite = ptr(in.Favourite != nil && *in.Favourite)
	return state
}

// Create builds one inbox. A preview calls no API method, because MailSlurp bills every inbox
// and counts it against the 30-day creation quota.
func (i *Inbox) Create(
	ctx context.Context, req infer.CreateRequest[InboxArgs],
) (infer.CreateResponse[InboxState], error) {
	if req.DryRun {
		preview := applyInputs(carried(req.Inputs), req.Inputs)
		preview.VirtualInbox = req.Inputs.VirtualInbox
		if pinned := text(req.Inputs.EmailAddress); pinned != nil {
			preview.EmailAddress = *pinned
		}
		return infer.CreateResponse[InboxState]{Output: preview}, nil
	}

	client, err := i.client()
	if err != nil {
		return infer.CreateResponse[InboxState]{}, err
	}
	dto, err := client.CreateInbox(ctx, createOptions(req.Inputs))
	if err != nil {
		return infer.CreateResponse[InboxState]{}, err
	}

	return infer.CreateResponse[InboxState]{
		ID:     dto.ID,
		Output: stateFrom(dto, carried(req.Inputs)),
	}, nil
}

// Read refreshes from GET /inboxes/{inboxId}, and answers the inputs as well so `pulumi import`
// builds a complete resource. The list projection reports a null inboxType, so it never uses it.
func (i *Inbox) Read(
	ctx context.Context, req infer.ReadRequest[InboxArgs, InboxState],
) (infer.ReadResponse[InboxArgs, InboxState], error) {
	client, err := i.client()
	if err != nil {
		return infer.ReadResponse[InboxArgs, InboxState]{}, err
	}

	dto, err := client.GetInbox(ctx, req.ID)
	if err != nil {
		// A deleted inbox is drift. An empty response tells the engine the inbox is gone.
		if IsGone(err) {
			return infer.ReadResponse[InboxArgs, InboxState]{}, nil
		}
		return infer.ReadResponse[InboxArgs, InboxState]{}, err
	}

	state := stateFrom(dto, req.State)
	return infer.ReadResponse[InboxArgs, InboxState]{
		ID:     req.ID,
		Inputs: inboxInputsFrom(state),
		State:  state,
	}, nil
}

// Update changes the five updatable properties in place. The create-only properties keep the
// values the state holds, because only a replacement can change them.
func (i *Inbox) Update(
	ctx context.Context, req infer.UpdateRequest[InboxArgs, InboxState],
) (infer.UpdateResponse[InboxState], error) {
	if req.DryRun {
		return infer.UpdateResponse[InboxState]{Output: applyInputs(req.State, req.Inputs)}, nil
	}

	client, err := i.client()
	if err != nil {
		return infer.UpdateResponse[InboxState]{}, err
	}
	dto, err := client.UpdateInbox(ctx, req.ID, updateOptions(req.Inputs))
	if err != nil {
		return infer.UpdateResponse[InboxState]{}, err
	}

	return infer.UpdateResponse[InboxState]{Output: stateFrom(dto, req.State)}, nil
}

// Delete removes the inbox. An inbox that is already gone is a success, not a failure.
func (i *Inbox) Delete(
	ctx context.Context, req infer.DeleteRequest[InboxState],
) (infer.DeleteResponse, error) {
	client, err := i.client()
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if err := client.DeleteInbox(ctx, req.ID); err != nil && !IsGone(err) {
		return infer.DeleteResponse{}, err
	}
	return infer.DeleteResponse{}, nil
}
