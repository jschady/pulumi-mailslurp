package internal

import (
	"context"
	"net/http"
	"net/url"
)

const webhooksPath = "/webhooks"

// WebhookHeader is one header MailSlurp sends with every webhook request.
type WebhookHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// WebhookHeaders is the header list the API requires whenever headers are present.
type WebhookHeaders struct {
	Headers []WebhookHeader `json:"headers"`
}

// WebhookDto is the webhook the API returns from the detail endpoint.
type WebhookDto struct {
	ID                            string          `json:"id"`
	BasicAuth                     bool            `json:"basicAuth"`
	Name                          *string         `json:"name"`
	PhoneID                       *string         `json:"phoneId"`
	InboxID                       *string         `json:"inboxId"`
	RequestBodyTemplate           *string         `json:"requestBodyTemplate"`
	URL                           string          `json:"url"`
	Method                        string          `json:"method"`
	PayloadJSONSchema             string          `json:"payloadJsonSchema"`
	CreatedAt                     *string         `json:"createdAt"`
	UpdatedAt                     string          `json:"updatedAt"`
	EventName                     string          `json:"eventName"`
	RequestHeaders                *WebhookHeaders `json:"requestHeaders"`
	IgnoreInsecureSSLCertificates *bool           `json:"ignoreInsecureSslCertificates"`
	UseStaticIPRange              *bool           `json:"useStaticIpRange"`
	HealthStatus                  *string         `json:"healthStatus"`
	Enabled                       *bool           `json:"enabled"`
}

// WebhookOptions is the whole webhook body. The update replaces the options, so every call
// carries the complete wanted state and eventName always travels.
type WebhookOptions struct {
	URL                           string          `json:"url"`
	EventName                     string          `json:"eventName"`
	Name                          string          `json:"name,omitempty"`
	RequestBodyTemplate           string          `json:"requestBodyTemplate,omitempty"`
	IncludeHeaders                *WebhookHeaders `json:"includeHeaders,omitempty"`
	Tags                          []string        `json:"tags,omitempty"`
	UseStaticIPRange              *bool           `json:"useStaticIpRange,omitempty"`
	IgnoreInsecureSSLCertificates *bool           `json:"ignoreInsecureSslCertificates,omitempty"`
}

func (c *client) CreateInboxWebhook(
	ctx context.Context, inboxID string, opts WebhookOptions,
) (*WebhookDto, error) {
	return call[WebhookDto](ctx, c, apiRequest{
		method:    http.MethodPost,
		path:      pathOf(inboxesPath, inboxID) + webhooksPath,
		operation: "createWebhook",
		body:      opts,
	})
}

// CreateAccountWebhook creates a webhook for every inbox in the account, and it creates no
// inbox, so it costs nothing against the inbox quota.
func (c *client) CreateAccountWebhook(
	ctx context.Context, opts WebhookOptions,
) (*WebhookDto, error) {
	return call[WebhookDto](ctx, c, apiRequest{
		method:    http.MethodPost,
		path:      webhooksPath,
		operation: "createAccountWebhook",
		body:      opts,
	})
}

// GetWebhook reads the detail endpoint. The list projection omits requestHeaders,
// requestBodyTemplate, method and payloadJsonSchema.
func (c *client) GetWebhook(ctx context.Context, webhookID string) (*WebhookDto, error) {
	return call[WebhookDto](ctx, c, apiRequest{
		method:    http.MethodGet,
		path:      pathOf(webhooksPath, webhookID),
		operation: "getWebhook",
	})
}

// UpdateWebhook replaces the whole options body: the server destroys every property the body
// omits, and it resets an omitted eventName. A non-nil inboxID moves the webhook to that inbox.
func (c *client) UpdateWebhook(
	ctx context.Context, webhookID string, inboxID *string, opts WebhookOptions,
) (*WebhookDto, error) {
	query := url.Values{}
	if inboxID != nil {
		query.Set("inboxId", *inboxID)
	}

	return call[WebhookDto](ctx, c, apiRequest{
		method:    http.MethodPatch,
		path:      pathOf(webhooksPath, webhookID),
		operation: "updateWebhook",
		query:     query,
		body:      opts,
	})
}

// UpdateWebhookHeaders writes the headers alone and leaves every other property as it is.
func (c *client) UpdateWebhookHeaders(
	ctx context.Context, webhookID string, headers WebhookHeaders,
) (*WebhookDto, error) {
	return call[WebhookDto](ctx, c, apiRequest{
		method:    http.MethodPut,
		path:      pathOf(webhooksPath, webhookID) + "/headers",
		operation: "updateWebhookHeaders",
		body:      headers,
	})
}

func (c *client) DeleteWebhook(ctx context.Context, webhookID string) error {
	return c.do(ctx, apiRequest{
		method:    http.MethodDelete,
		path:      pathOf(webhooksPath, webhookID),
		operation: "deleteWebhookById",
	}, nil)
}
