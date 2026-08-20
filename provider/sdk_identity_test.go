package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaLanguage is the per-language block each SDK generator reads out of the schema. The
// generators bake these names into the manifests, and nothing downstream can correct them.
type schemaLanguage struct {
	CSharp struct {
		RootNamespace string `json:"rootNamespace"`
	} `json:"csharp"`
	Go struct {
		ImportBasePath string `json:"importBasePath"`
	} `json:"go"`
	NodeJS struct {
		PackageName string `json:"packageName"`
	} `json:"nodejs"`
	Python struct {
		PackageName string `json:"packageName"`
	} `json:"python"`
}

func languageBlock(t *testing.T, raw []byte) schemaLanguage {
	t.Helper()
	var parsed struct {
		Language schemaLanguage `json:"language"`
	}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	return parsed.Language
}

// The name each generator writes into its manifest. A generator left to its defaults derives
// `@jschady/mailslurp` and `jschady_mailslurp`, which are not the names this provider publishes.
const (
	npmPackageName   = "pulumi-mailslurp"
	pythonModuleName = "pulumi_mailslurp"
	goImportBasePath = "github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp"
)

// TestTheSchemaNamesThePublishedPackages reads the identities out of both the committed schema
// and the running provider. The committed file is what generation reads; the provider is its source.
func TestTheSchemaNamesThePublishedPackages(t *testing.T) {
	for source, raw := range map[string][]byte{
		committedSchemaFile:   committedSchema(t),
		"the running builder": builtSchema(t),
	} {
		language := languageBlock(t, raw)
		assert.Equal(t, goImportBasePath, language.Go.ImportBasePath,
			"%s must name a Go import path a program can compile", source)
		assert.Equal(t, npmPackageName, language.NodeJS.PackageName,
			"%s must name the npm package this provider publishes", source)
		assert.Equal(t, pythonModuleName, language.Python.PackageName,
			"%s must name the Python package this provider publishes", source)
	}
}

// TestTheDotnetNamespaceComesFromThePublisher pins the one identity nothing overrides. The
// generator derives it, so an override added later would be dead weight nobody reads.
func TestTheDotnetNamespaceComesFromThePublisher(t *testing.T) {
	var parsed struct {
		Publisher string `json:"publisher"`
		Name      string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &parsed))
	assert.Equal(t, "jschady", parsed.Publisher)
	assert.Equal(t, "mailslurp", parsed.Name)
	assert.Empty(t, languageBlock(t, committedSchema(t)).CSharp.RootNamespace,
		"the schema must not override the .NET root namespace the publisher already gives")
}
