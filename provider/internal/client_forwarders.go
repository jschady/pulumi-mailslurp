package internal

import (
	"context"
	"net/http"
	"net/url"
)

const forwardersPath = "/forwarders"

// ForwarderMatch is one leaf rule in the forwarder match tree.
type ForwarderMatch struct {
	Field  string `json:"field"`
	Should string `json:"should"`
	Value  string `json:"value"`
}

// ForwarderMatchOptions is the match tree the forwarder applies to each email.
type ForwarderMatchOptions struct {
	Operator string                  `json:"operator"`
	Matches  []ForwarderMatch        `json:"matches,omitempty"`
	Groups   []ForwarderMatchOptions `json:"groups,omitempty"`
}

// ForwarderDto is the inbox forwarder the API returns. The server owns the name.
type ForwarderDto struct {
	ID                  string                 `json:"id"`
	InboxID             *string                `json:"inboxId"`
	Name                *string                `json:"name"`
	Field               *string                `json:"field"`
	Match               *string                `json:"match"`
	Should              *string                `json:"should"`
	ForwardToRecipients []string               `json:"forwardToRecipients"`
	MatchOptions        *ForwarderMatchOptions `json:"matchOptions"`
	CreatedAt           string                 `json:"createdAt"`
}

// ForwarderOptions is the whole forwarder body, shared by the create and the update.
type ForwarderOptions struct {
	Field               *string                `json:"field,omitempty"`
	Should              *string                `json:"should,omitempty"`
	Match               *string                `json:"match,omitempty"`
	ForwardToRecipients []string               `json:"forwardToRecipients"`
	MatchOptions        *ForwarderMatchOptions `json:"matchOptions,omitempty"`
}

// CreateForwarder creates the forwarder for one inbox. The inbox is a create-time query
// parameter, so moving a forwarder means replacing it.
func (c *client) CreateForwarder(
	ctx context.Context, inboxID string, opts ForwarderOptions,
) (*ForwarderDto, error) {
	query := url.Values{}
	query.Set("inboxId", inboxID)

	return call[ForwarderDto](ctx, c, apiRequest{
		method:    http.MethodPost,
		path:      forwardersPath,
		operation: "createNewInboxForwarder",
		query:     query,
		body:      opts,
	})
}

func (c *client) GetForwarder(ctx context.Context, forwarderID string) (*ForwarderDto, error) {
	return call[ForwarderDto](ctx, c, apiRequest{
		method:    http.MethodGet,
		path:      pathOf(forwardersPath, forwarderID),
		operation: "getInboxForwarder",
	})
}

func (c *client) UpdateForwarder(
	ctx context.Context, forwarderID string, opts ForwarderOptions,
) (*ForwarderDto, error) {
	return call[ForwarderDto](ctx, c, apiRequest{
		method:    http.MethodPut,
		path:      pathOf(forwardersPath, forwarderID),
		operation: "updateInboxForwarder",
		body:      opts,
	})
}

func (c *client) DeleteForwarder(ctx context.Context, forwarderID string) error {
	return c.do(ctx, apiRequest{
		method:    http.MethodDelete,
		path:      pathOf(forwardersPath, forwarderID),
		operation: "deleteInboxForwarder",
	}, nil)
}
