//go:generate go run go.uber.org/mock/mockgen -typed -package internal -source client.go -destination mockclient_test.go --self_package github.com/jschady/pulumi-mailslurp/provider/internal

package internal

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the MailSlurp surface the provider uses, and the only mock boundary in this package.
type Client interface {
	CreateInbox(ctx context.Context, opts CreateInboxOptions) (*InboxDto, error)
	GetInbox(ctx context.Context, inboxID string) (*InboxDto, error)
	UpdateInbox(ctx context.Context, inboxID string, opts UpdateInboxOptions) (*InboxDto, error)
	DeleteInbox(ctx context.Context, inboxID string) error

	CreateInboxWebhook(ctx context.Context, inboxID string, opts WebhookOptions) (*WebhookDto, error)
	CreateAccountWebhook(ctx context.Context, opts WebhookOptions) (*WebhookDto, error)
	GetWebhook(ctx context.Context, webhookID string) (*WebhookDto, error)
	UpdateWebhook(ctx context.Context, webhookID string, inboxID *string, opts WebhookOptions) (*WebhookDto, error)
	UpdateWebhookHeaders(ctx context.Context, webhookID string, headers WebhookHeaders) (*WebhookDto, error)
	DeleteWebhook(ctx context.Context, webhookID string) error

	CreateTemplate(ctx context.Context, opts TemplateOptions) (*TemplateDto, error)
	GetTemplate(ctx context.Context, templateID string) (*TemplateDto, error)
	UpdateTemplate(ctx context.Context, templateID string, opts TemplateOptions) (*TemplateDto, error)
	DeleteTemplate(ctx context.Context, templateID string) error

	CreateRuleset(ctx context.Context, inboxID string, opts RulesetOptions) (*RulesetDto, error)
	GetRuleset(ctx context.Context, rulesetID string) (*RulesetDto, error)
	DeleteRuleset(ctx context.Context, rulesetID string) error

	CreateForwarder(ctx context.Context, inboxID string, opts ForwarderOptions) (*ForwarderDto, error)
	GetForwarder(ctx context.Context, forwarderID string) (*ForwarderDto, error)
	UpdateForwarder(ctx context.Context, forwarderID string, opts ForwarderOptions) (*ForwarderDto, error)
	DeleteForwarder(ctx context.Context, forwarderID string) error

	GetDomain(ctx context.Context, domainID string, checkForErrors bool) (*DomainDto, error)
}

const (
	// The per-language vanity hosts also resolve, and this one is the host the API documents.
	defaultBaseURL  = "https://api.mailslurp.com"
	apiKeyHeader    = "x-api-key"
	contentTypeJSON = "application/json"
	requestTimeout  = 60 * time.Second
)

type client struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

var _ Client = (*client)(nil)

// NewClient builds a client for one endpoint and one API key.
func NewClient(endpoint, apiKey string) (Client, error) {
	return newClient(endpoint, apiKey)
}

func newClient(endpoint, apiKey string) (*client, error) {
	base := strings.TrimRight(endpoint, "/")
	if base == "" {
		base = defaultBaseURL
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, fmt.Errorf("the MailSlurp endpoint is not a URL: %w", err)
	}

	return &client{
		http:    &http.Client{Timeout: requestTimeout},
		baseURL: base,
		apiKey:  apiKey,
	}, nil
}
