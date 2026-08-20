package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// DomainType selects the inbox kind a custom domain serves. It never shares a Go type with
// InboxType: the API spells the second value differently for the two concepts.
type DomainType string

const (
	// DomainTypeHTTP serves inboxes that store email with HTTP endpoints.
	DomainTypeHTTP DomainType = "HTTP_INBOX"
	// DomainTypeSMTP serves inboxes that receive email on a real SMTP mail server.
	DomainTypeSMTP DomainType = "SMTP_DOMAIN"
)

// Values lists the allowed domain types for the schema.
func (DomainType) Values() []infer.EnumValue[DomainType] {
	return []infer.EnumValue[DomainType]{
		{
			Name:        "HttpInbox",
			Value:       DomainTypeHTTP,
			Description: "The domain serves inboxes that store email with HTTP endpoints.",
		},
		{
			Name:        "SmtpDomain",
			Value:       DomainTypeSMTP,
			Description: "The domain serves inboxes that receive email on a real SMTP mail server.",
		},
	}
}

const (
	inboxNotFoundMessage = "MailSlurp has no inbox with the identifier `%s`. " +
		"Check the `inboxId` property and try again."
	domainNotFoundMessage = "MailSlurp has no domain with the identifier `%s`. " +
		"Check the `domainId` property and try again."
	blankInboxIDMessage = "The `inboxId` property is empty. " +
		"Set the identifier of the inbox to read and try again."
	blankDomainIDMessage = "The `domainId` property is empty. " +
		"Set the identifier of the domain to read and try again."
)

// blankIdentifier reports whether an identifier names nothing. The transport guard refuses one too
// and stays the backstop; this check names the property the stack must set.
func blankIdentifier(id string) bool {
	return strings.TrimSpace(id) == ""
}

// functionClient answers the configured client, or the diagnostic that names what is missing.
func functionClient(config *Config) (Client, error) {
	if config == nil || config.client == nil {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return nil, errors.New(notConfiguredMessage)
	}
	return config.client, nil
}

// listOrEmpty answers an empty list for a null API list, so a stack loops over it with no nil check.
func listOrEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// GetInbox reads one inbox.
type GetInbox struct {
	config *Config
}

// Annotate provides the user-facing description of the getInbox function.
func (f *GetInbox) Annotate(a infer.Annotator) {
	a.Describe(&f, dedent(`
		The function reads one MailSlurp inbox by its identifier. Use it when a
		stack must reference an inbox that Pulumi does not manage. The function
		reads the inbox and changes nothing.
	`))
}

// GetInboxArgs are the inputs of the getInbox function.
type GetInboxArgs struct {
	InboxID string `pulumi:"inboxId"`
}

// Annotate provides the user-facing description of the getInbox input.
func (ia *GetInboxArgs) Annotate(a infer.Annotator) {
	a.Describe(&ia.InboxID, dedent(`
		The identifier of the inbox to read. MailSlurp assigns this value when
		it creates the inbox.
	`))
}

// GetInboxResult is the inbox the getInbox function returns. It carries `inboxId` rather than
// `id`, because Pulumi reserves `id` for resources.
type GetInboxResult struct {
	InboxID       string     `pulumi:"inboxId"`
	CreatedAt     string     `pulumi:"createdAt"`
	Name          *string    `pulumi:"name,optional"`
	Description   *string    `pulumi:"description,optional"`
	DomainID      *string    `pulumi:"domainId,optional"`
	EmailAddress  string     `pulumi:"emailAddress"`
	ExpiresAt     *string    `pulumi:"expiresAt,optional"`
	Favourite     bool       `pulumi:"favourite"`
	Tags          []string   `pulumi:"tags"`
	InboxType     *InboxType `pulumi:"inboxType,optional"`
	ReadOnly      bool       `pulumi:"readOnly"`
	VirtualInbox  bool       `pulumi:"virtualInbox"`
	FunctionsAs   *string    `pulumi:"functionsAs,optional"`
	LocalPart     *string    `pulumi:"localPart,optional"`
	Domain        *string    `pulumi:"domain,optional"`
	AccountRegion *string    `pulumi:"accountRegion,optional"`
}

// Annotate provides the user-facing descriptions of the getInbox outputs.
func (ir *GetInboxResult) Annotate(a infer.Annotator) {
	a.Describe(&ir.InboxID, "The identifier of the inbox.")
	a.Describe(&ir.CreatedAt, "The time when MailSlurp created the inbox.")
	a.Describe(&ir.Name, "The display name of the inbox.")
	a.Describe(&ir.Description, "The free-text description of the inbox.")
	a.Describe(&ir.DomainID, "The identifier of the custom domain that provides the inbox address.")
	a.Describe(&ir.EmailAddress, "The full email address of the inbox.")
	a.Describe(&ir.ExpiresAt, "The time when MailSlurp expires the inbox, in RFC 3339 format.")
	a.Describe(&ir.Favourite, "MailSlurp reports whether the inbox is a favourite in the dashboard.")
	a.Describe(&ir.Tags, "The tags on the inbox.")
	a.Describe(&ir.InboxType, "The inbox type.")
	a.Describe(&ir.ReadOnly, "MailSlurp reports whether the inbox is read-only.")
	a.Describe(&ir.VirtualInbox,
		"MailSlurp reports whether the inbox is virtual. A virtual inbox never sends email to real recipients.")
	a.Describe(&ir.FunctionsAs, "The special function of the inbox, for example an alias or a catch-all.")
	a.Describe(&ir.LocalPart, "The local part of the inbox email address.")
	a.Describe(&ir.Domain, "The domain part of the inbox email address.")
	a.Describe(&ir.AccountRegion, "The region of the account that holds the inbox.")
}

// Invoke reads the inbox. A missing inbox fails the call, because an empty result would hand the
// stack a silent blank.
func (f *GetInbox) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetInboxArgs],
) (infer.FunctionResponse[GetInboxResult], error) {
	if blankIdentifier(req.Input.InboxID) {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return infer.FunctionResponse[GetInboxResult]{}, errors.New(blankInboxIDMessage)
	}

	client, err := functionClient(f.config)
	if err != nil {
		return infer.FunctionResponse[GetInboxResult]{}, err
	}

	dto, err := client.GetInbox(ctx, req.Input.InboxID)
	if err != nil {
		if IsGone(err) {
			//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
			return infer.FunctionResponse[GetInboxResult]{},
				fmt.Errorf(inboxNotFoundMessage, req.Input.InboxID)
		}
		return infer.FunctionResponse[GetInboxResult]{}, err
	}

	return infer.FunctionResponse[GetInboxResult]{Output: inboxResult(dto)}, nil
}

func inboxResult(dto *InboxDto) GetInboxResult {
	result := GetInboxResult{
		InboxID:       dto.ID,
		CreatedAt:     dto.CreatedAt,
		Name:          text(dto.Name),
		Description:   text(dto.Description),
		DomainID:      text(dto.DomainID),
		EmailAddress:  dto.EmailAddress,
		ExpiresAt:     text(dto.ExpiresAt),
		Favourite:     dto.Favourite,
		Tags:          listOrEmpty(dto.Tags),
		ReadOnly:      dto.ReadOnly,
		VirtualInbox:  dto.VirtualInbox,
		FunctionsAs:   text(dto.FunctionsAs),
		LocalPart:     text(dto.LocalPart),
		Domain:        text(dto.Domain),
		AccountRegion: text(dto.AccountRegion),
	}
	if reported := text(dto.InboxType); reported != nil {
		result.InboxType = ptr(InboxType(*reported))
	}
	return result
}

// GetDomain reads one custom domain.
type GetDomain struct {
	config *Config
}

// Annotate provides the user-facing description of the getDomain function.
func (f *GetDomain) Annotate(a infer.Annotator) {
	a.Describe(&f, dedent(`
		The function reads one MailSlurp custom domain by its identifier. The
		result carries the DNS records that the domain needs, so a stack can
		create them with a DNS provider. The function reads the domain and
		changes nothing.
	`))
}

// GetDomainArgs are the inputs of the getDomain function.
type GetDomainArgs struct {
	DomainID       string `pulumi:"domainId"`
	CheckForErrors *bool  `pulumi:"checkForErrors,optional"`
}

// Annotate provides the user-facing descriptions of the getDomain inputs.
func (da *GetDomainArgs) Annotate(a infer.Annotator) {
	a.Describe(&da.DomainID, dedent(`
		The identifier of the domain to read. MailSlurp assigns this value when
		it creates the domain.
	`))
	a.Describe(&da.CheckForErrors, dedent(`
		If you set this property, MailSlurp checks the domain for DNS record
		problems. The default is "false".
	`))
}

// DomainNameRecordResult is one DNS record the domain needs.
type DomainNameRecordResult struct {
	Label                    string   `pulumi:"label"`
	Required                 bool     `pulumi:"required"`
	RecordType               string   `pulumi:"recordType"`
	Name                     string   `pulumi:"name"`
	RecordEntries            []string `pulumi:"recordEntries"`
	TTL                      int      `pulumi:"ttl"`
	AlternativeRecordEntries []string `pulumi:"alternativeRecordEntries"`
}

// Annotate provides the user-facing descriptions of one DNS record.
func (dr *DomainNameRecordResult) Annotate(a infer.Annotator) {
	a.Describe(&dr.Label, dedent(`
		The role of the record, for example "VERIFICATION" or "DKIM".
	`))
	a.Describe(&dr.Required, "MailSlurp reports whether the domain needs this record.")
	a.Describe(&dr.RecordType, dedent(`
		The DNS record type, for example "TXT", "MX", or "CNAME".
	`))
	a.Describe(&dr.Name, "The full DNS name of the record.")
	a.Describe(&dr.RecordEntries, "The values of the record. Add one entry for each value.")
	a.Describe(&dr.TTL, "The time to live of the record, in seconds.")
	a.Describe(&dr.AlternativeRecordEntries,
		"The values to use when your DNS provider rejects the main entries.")
}

// GetDomainResult is the custom domain the getDomain function returns. It carries `domainId`
// rather than `id`, because Pulumi reserves `id` for resources.
type GetDomainResult struct {
	DomainID                string                   `pulumi:"domainId"`
	Domain                  string                   `pulumi:"domain"`
	DomainType              DomainType               `pulumi:"domainType"`
	VerificationToken       string                   `pulumi:"verificationToken"`
	DkimTokens              []string                 `pulumi:"dkimTokens"`
	IsVerified              bool                     `pulumi:"isVerified"`
	HasMissingRecords       bool                     `pulumi:"hasMissingRecords"`
	MissingRecordsMessage   *string                  `pulumi:"missingRecordsMessage,optional"`
	HasDuplicateRecords     bool                     `pulumi:"hasDuplicateRecords"`
	DuplicateRecordsMessage *string                  `pulumi:"duplicateRecordsMessage,optional"`
	DomainNameRecords       []DomainNameRecordResult `pulumi:"domainNameRecords"`
	CatchAllInboxID         *string                  `pulumi:"catchAllInboxId,optional"`
	CreatedAt               string                   `pulumi:"createdAt"`
	UpdatedAt               string                   `pulumi:"updatedAt"`
}

// Annotate provides the user-facing descriptions of the getDomain outputs.
func (dr *GetDomainResult) Annotate(a infer.Annotator) {
	a.Describe(&dr.DomainID, "The identifier of the domain.")
	a.Describe(&dr.Domain, "The custom domain name.")
	a.Describe(&dr.DomainType, "The domain type.")
	a.Describe(&dr.VerificationToken, dedent(`
		The value that proves you own the domain. Add it to the DNS records of
		the domain.
	`))
	a.Describe(&dr.DkimTokens, dedent(`
		The DKIM values of the domain. Add one CNAME record for each value.
	`))
	a.Describe(&dr.IsVerified, "MailSlurp reports whether the domain passed the DNS checks.")
	a.Describe(&dr.HasMissingRecords, "MailSlurp reports whether the domain misses a required DNS record.")
	a.Describe(&dr.MissingRecordsMessage, "The message that names the missing DNS records.")
	a.Describe(&dr.HasDuplicateRecords, "MailSlurp reports whether the domain has a duplicate DNS record.")
	a.Describe(&dr.DuplicateRecordsMessage, "The message that names the duplicate DNS records.")
	a.Describe(&dr.DomainNameRecords, dedent(`
		The DNS records that the domain needs. Loop over this list to create the
		records with a DNS provider.
	`))
	a.Describe(&dr.CatchAllInboxID, dedent(`
		The identifier of the inbox that receives the email of every unmatched
		address.
	`))
	a.Describe(&dr.CreatedAt, "The time when MailSlurp created the domain.")
	a.Describe(&dr.UpdatedAt, "The time when MailSlurp last changed the domain.")
}

// Invoke reads the custom domain. A missing domain fails the call, because an empty result would
// hand the stack a silent blank.
func (f *GetDomain) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetDomainArgs],
) (infer.FunctionResponse[GetDomainResult], error) {
	if blankIdentifier(req.Input.DomainID) {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return infer.FunctionResponse[GetDomainResult]{}, errors.New(blankDomainIDMessage)
	}

	client, err := functionClient(f.config)
	if err != nil {
		return infer.FunctionResponse[GetDomainResult]{}, err
	}

	check := req.Input.CheckForErrors != nil && *req.Input.CheckForErrors
	dto, err := client.GetDomain(ctx, req.Input.DomainID, check)
	if err != nil {
		if IsGone(err) {
			//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
			return infer.FunctionResponse[GetDomainResult]{},
				fmt.Errorf(domainNotFoundMessage, req.Input.DomainID)
		}
		return infer.FunctionResponse[GetDomainResult]{}, err
	}

	return infer.FunctionResponse[GetDomainResult]{Output: domainResult(dto)}, nil
}

func domainResult(dto *DomainDto) GetDomainResult {
	records := make([]DomainNameRecordResult, 0, len(dto.DomainNameRecords))
	for _, record := range dto.DomainNameRecords {
		records = append(records, DomainNameRecordResult{
			Label:                    record.Label,
			Required:                 record.Required,
			RecordType:               record.RecordType,
			Name:                     record.Name,
			RecordEntries:            listOrEmpty(record.RecordEntries),
			TTL:                      record.TTL,
			AlternativeRecordEntries: listOrEmpty(record.AlternativeRecordEntries),
		})
	}

	return GetDomainResult{
		DomainID:                dto.ID,
		Domain:                  dto.Domain,
		DomainType:              DomainType(dto.DomainType),
		VerificationToken:       dto.VerificationToken,
		DkimTokens:              listOrEmpty(dto.DkimTokens),
		IsVerified:              dto.IsVerified,
		HasMissingRecords:       dto.HasMissingRecords,
		MissingRecordsMessage:   text(dto.MissingRecordsMessage),
		HasDuplicateRecords:     dto.HasDuplicateRecords,
		DuplicateRecordsMessage: text(dto.DuplicateRecordsMsg),
		DomainNameRecords:       records,
		CatchAllInboxID:         text(dto.CatchAllInboxID),
		CreatedAt:               dto.CreatedAt,
		UpdatedAt:               dto.UpdatedAt,
	}
}
