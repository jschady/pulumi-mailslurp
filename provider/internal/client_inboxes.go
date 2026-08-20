package internal

import (
	"context"
	"net/http"
)

const inboxesPath = "/inboxes"

// InboxDto is the inbox the API returns from the detail endpoint.
type InboxDto struct {
	ID            string   `json:"id"`
	CreatedAt     string   `json:"createdAt"`
	Name          *string  `json:"name"`
	Description   *string  `json:"description"`
	DomainID      *string  `json:"domainId"`
	EmailAddress  string   `json:"emailAddress"`
	ExpiresAt     *string  `json:"expiresAt"`
	Favourite     bool     `json:"favourite"`
	Tags          []string `json:"tags"`
	InboxType     *string  `json:"inboxType"`
	ReadOnly      bool     `json:"readOnly"`
	VirtualInbox  bool     `json:"virtualInbox"`
	FunctionsAs   *string  `json:"functionsAs"`
	LocalPart     *string  `json:"localPart"`
	Domain        *string  `json:"domain"`
	AccountRegion *string  `json:"accountRegion"`
}

// CreateInboxOptions is the create body. Every property is optional.
type CreateInboxOptions struct {
	Name            *string  `json:"name,omitempty"`
	Description     *string  `json:"description,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	EmailAddress    *string  `json:"emailAddress,omitempty"`
	DomainName      *string  `json:"domainName,omitempty"`
	DomainID        *string  `json:"domainId,omitempty"`
	ExpiresAt       *string  `json:"expiresAt,omitempty"`
	ExpiresIn       *int64   `json:"expiresIn,omitempty"`
	Favourite       *bool    `json:"favourite,omitempty"`
	InboxType       *string  `json:"inboxType,omitempty"`
	Prefix          *string  `json:"prefix,omitempty"`
	UseDomainPool   *bool    `json:"useDomainPool,omitempty"`
	UseShortAddress *bool    `json:"useShortAddress,omitempty"`
	VirtualInbox    *bool    `json:"virtualInbox,omitempty"`
}

// UpdateInboxOptions is the sparse merge-patch body: an absent property stays as it is.
type UpdateInboxOptions struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
	ExpiresAt   *string   `json:"expiresAt,omitempty"`
	Favourite   *bool     `json:"favourite,omitempty"`
}

func (c *client) CreateInbox(ctx context.Context, opts CreateInboxOptions) (*InboxDto, error) {
	return call[InboxDto](ctx, c, apiRequest{
		method:    http.MethodPost,
		path:      inboxesPath + "/withOptions",
		operation: "createInboxWithOptions",
		body:      opts,
	})
}

// GetInbox reads the detail endpoint. The list projection answers a null inboxType for a
// catch-all inbox, which would loop a diff forever.
func (c *client) GetInbox(ctx context.Context, inboxID string) (*InboxDto, error) {
	return call[InboxDto](ctx, c, apiRequest{
		method:    http.MethodGet,
		path:      pathOf(inboxesPath, inboxID),
		operation: "getInbox",
	})
}

func (c *client) UpdateInbox(
	ctx context.Context, inboxID string, opts UpdateInboxOptions,
) (*InboxDto, error) {
	return call[InboxDto](ctx, c, apiRequest{
		method:    http.MethodPatch,
		path:      pathOf(inboxesPath, inboxID),
		operation: "updateInbox",
		body:      opts,
	})
}

func (c *client) DeleteInbox(ctx context.Context, inboxID string) error {
	return c.do(ctx, apiRequest{
		method:    http.MethodDelete,
		path:      pathOf(inboxesPath, inboxID),
		operation: "deleteInbox",
	}, nil)
}
