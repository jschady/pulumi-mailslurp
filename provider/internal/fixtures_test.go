package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test values and helpers that more than one resource reads. A literal repeated across files trips
// the goconst linter, and a second copy of a helper drifts from the first, so both live here.

// The two shapes of a provider that carries no usable client: Configure never ran, or it ran and
// failed before it built the client.
const (
	titleWithoutConfiguration = "no provider configuration"
	titleWithoutClient        = "a configuration without a client"
)

// The import paths the resource source scans read. A resource that reaches MailSlurp through the
// Client interface alone imports nothing else.
const (
	contextImportPath = "context"
	errorsImportPath  = "errors"
	embedImportPath   = "embed"
	inferImportPath   = "github.com/pulumi/pulumi-go-provider/infer"
)

// wildcardTarget is a catch-all address a live account can hold. Every sweep predicate must leave
// it alone, because this package never builds one.
const wildcardTarget = "*@example.com"

// The two output names that more than one resource carries.
const (
	propCreatedAt = "createdAt"
	propReadOnly  = "readOnly"
)

// requireNotConfigured checks the whole diagnostic, not a phrase inside it: every unconfigured
// path must answer the one sentence that tells a user which value to set.
func requireNotConfigured(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.Contains(t, err.Error(), notConfiguredMessage)
}
