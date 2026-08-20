package internal

import (
	"context"
	"net/http"
	"net/url"
)

const domainsPath = "/domains"

// DomainNameRecord is one DNS record MailSlurp needs for a custom domain.
type DomainNameRecord struct {
	Label                    string   `json:"label"`
	Required                 bool     `json:"required"`
	RecordType               string   `json:"recordType"`
	Name                     string   `json:"name"`
	RecordEntries            []string `json:"recordEntries"`
	TTL                      int      `json:"ttl"`
	AlternativeRecordEntries []string `json:"alternativeRecordEntries"`
}

// DomainDto is the custom domain the API returns, including the records a DNS provider needs.
type DomainDto struct {
	ID                    string             `json:"id"`
	Domain                string             `json:"domain"`
	VerificationToken     string             `json:"verificationToken"`
	DkimTokens            []string           `json:"dkimTokens"`
	DuplicateRecordsMsg   *string            `json:"duplicateRecordsMessage"`
	HasDuplicateRecords   bool               `json:"hasDuplicateRecords"`
	MissingRecordsMessage *string            `json:"missingRecordsMessage"`
	HasMissingRecords     bool               `json:"hasMissingRecords"`
	IsVerified            bool               `json:"isVerified"`
	DomainNameRecords     []DomainNameRecord `json:"domainNameRecords"`
	CatchAllInboxID       *string            `json:"catchAllInboxId"`
	CreatedAt             string             `json:"createdAt"`
	UpdatedAt             string             `json:"updatedAt"`
	DomainType            string             `json:"domainType"`
}

func (c *client) GetDomain(
	ctx context.Context, domainID string, checkForErrors bool,
) (*DomainDto, error) {
	query := url.Values{}
	if checkForErrors {
		query.Set("checkForErrors", "true")
	}

	return call[DomainDto](ctx, c, apiRequest{
		method:    http.MethodGet,
		path:      pathOf(domainsPath, domainID),
		operation: "getDomain",
		query:     query,
	})
}
