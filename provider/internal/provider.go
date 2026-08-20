package internal

import (
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

// respectSchemaVersion makes each generator take the version from the schema rather than
// stamping one of its own. The builder sets it for all four languages and this map keeps it.
const respectSchemaVersion = "respectSchemaVersion"

// languageMap carries the framework defaults plus the two package names the generators would
// otherwise derive from the namespace, giving `@jschady/mailslurp` and `jschady_mailslurp`.
func languageMap() map[string]any {
	return map[string]any{
		"nodejs": map[string]any{
			"packageName": "pulumi-mailslurp",
			// The generator writes package.json without a description unless this key
			// carries one, and npm then shows the head of the README in its place.
			"packageDescription": "A Pulumi provider that creates and manages MailSlurp email infrastructure.",
			respectSchemaVersion: true,
		},
		"go": map[string]any{
			"generateResourceContainerTypes": true,
			respectSchemaVersion:             true,
		},
		"python": map[string]any{
			"packageName":        "pulumi_mailslurp",
			respectSchemaVersion: true,
			"pyproject":          map[string]any{"enabled": true},
		},
		// The .NET generator derives the Jschady.Mailslurp namespace from the publisher,
		// so an explicit rootNamespace here would only repeat it.
		"csharp": map[string]any{
			respectSchemaVersion: true,
		},
	}
}

// NewProvider builds the MailSlurp provider with the given client factory.
func NewProvider(cf clientF) (p.Provider, error) {
	config := &Config{clientF: cf}
	return infer.NewProviderBuilder().
		WithDisplayName("MailSlurp").
		WithDescription("A Pulumi provider that creates and manages MailSlurp email infrastructure.").
		WithKeywords("pulumi", "mailslurp", "email", "category/utility", "kind/native").
		WithHomepage("https://www.mailslurp.com").
		WithLicense("Apache-2.0").
		WithPublisher("jschady").
		WithNamespace("jschady").
		WithRepository("https://github.com/jschady/pulumi-mailslurp").
		WithPluginDownloadURL("github://api.github.com/jschady/pulumi-mailslurp").
		WithLogoURL("https://raw.githubusercontent.com/jschady/pulumi-mailslurp/main/docs/logo.svg").
		WithModuleMap(map[tokens.ModuleName]tokens.ModuleName{"internal": "index"}).
		// The builder otherwise derives the import path from the repository URL, scheme and
		// all, and Go rejects `https:/github.com/...` as an import path.
		WithLanguageMap(languageMap()).
		WithGoImportPath("github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp").
		WithConfig(infer.Config(config)).
		WithResources(
			infer.Resource(&Inbox{config: config}),
			infer.Resource(&Webhook{config: config}),
			infer.Resource(&InboxRuleset{config: config}),
			infer.Resource(&EmailTemplate{config: config}),
			infer.Resource(&InboxForwarder{config: config}),
		).
		WithFunctions(
			infer.Function(&GetInbox{config: config}),
			infer.Function(&GetDomain{config: config}),
		).
		Build()
}
