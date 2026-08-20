package internal

import (
	"context"
	"errors"
	"os"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

//nolint:gosec // G101: this names the environment variable, and holds no credential.
const apiKeyEnvironmentVariable = "MAILSLURP_API_KEY"

// clientF builds the MailSlurp client for a configured provider; tests inject fakes here.
type clientF func(context.Context, *Config) (Client, error)

// RealClientF builds the production client from the provider configuration.
func RealClientF(_ context.Context, cfg *Config) (Client, error) {
	return NewClient(cfg.Endpoint, cfg.APIKey)
}

// Config is the provider configuration shared by reference with every resource.
type Config struct {
	APIKey   string `pulumi:"apiKey,optional" provider:"secret"`
	Endpoint string `pulumi:"endpoint,optional"`

	clientF clientF
	client  Client
}

// Annotate provides the user-facing descriptions and defaults for the provider configuration.
func (c *Config) Annotate(a infer.Annotator) {
	a.Describe(&c.APIKey, dedent(`
		The MailSlurp API key. The provider reads "MAILSLURP_API_KEY" when you
		do not set this property. The key is a secret, and Pulumi encrypts it
		in the state.
	`))
	a.SetDefault(&c.APIKey, "", apiKeyEnvironmentVariable)
	a.Describe(&c.Endpoint, dedent(`
		The base URL of the MailSlurp API. The default is
		"https://api.mailslurp.com". Set this property only when you test
		against a different endpoint.
	`))
	a.SetDefault(&c.Endpoint, "https://api.mailslurp.com")
}

// Diff keeps configuration changes as updates; the framework default replaces
// the provider on any config change (pulumi-go-provider#409).
func (c *Config) Diff(_ context.Context, req infer.DiffRequest[*Config, *Config]) (infer.DiffResponse, error) {
	old, news := req.State, req.Inputs
	if old == nil {
		old = &Config{}
	}
	if news == nil {
		news = &Config{}
	}
	detailed := map[string]p.PropertyDiff{}
	if old.APIKey != news.APIKey {
		detailed["apiKey"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if old.Endpoint != news.Endpoint {
		detailed["endpoint"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	return infer.DiffResponse{HasChanges: len(detailed) > 0, DetailedDiff: detailed}, nil
}

const missingKeyMessage = "The MailSlurp API key is missing. Set `MAILSLURP_API_KEY` and try again."

// Configure validates the configuration and builds the API client once per process.
func (c *Config) Configure(ctx context.Context) error {
	if c.APIKey == "" {
		// An import writes a default provider that holds no configuration at all, and the engine
		// applies the schema default to a program run alone.
		c.APIKey = os.Getenv(apiKeyEnvironmentVariable)
	}
	if c.APIKey == "" {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return errors.New(missingKeyMessage)
	}
	build := c.clientF
	if build == nil {
		build = RealClientF
	}
	client, err := build(ctx, c)
	if err != nil {
		return err
	}
	c.client = client
	return nil
}
