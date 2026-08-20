package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

// New takes the host client of the Pulumi SDK, and the upgrade tests attach with a nil host. The
// server must build and answer the schema without one.
func TestNewBuildsAWorkingServerWithoutAHost(t *testing.T) {
	t.Parallel()
	server, err := New(nil)
	require.NoError(t, err)
	require.NotNil(t, server)

	resp, err := server.GetSchema(context.Background(), &rpc.GetSchemaRequest{Version: 0})
	require.NoError(t, err)
	assert.Contains(t, resp.GetSchema(), `"mailslurp:index:Inbox"`)
}

func TestNewAnswersOneServerPerCall(t *testing.T) {
	t.Parallel()
	first, err := New(nil)
	require.NoError(t, err)
	second, err := New(nil)
	require.NoError(t, err)
	assert.NotSame(t, first, second, "each call must build its own server")
}

// The provider name is the plugin name, the schema package name, and the token prefix. A change
// here renames the published package, so the value is pinned.
func TestTheProviderNameIsTheSchemaPackageName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "mailslurp", Name)
}

// packageCalls answers every package-qualified call one source file makes.
func packageCalls(t *testing.T, parts ...string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(),
		filepath.Join(append([]string{repoRoot(t)}, parts...)...), nil, 0)
	require.NoError(t, err)

	var called []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				called = append(called, pkg.Name+"."+sel.Sel.Name)
			}
		}
		return true
	})
	return called
}

// The plugin binary must reach Serve, and Serve must reach the gRPC entry point of the Pulumi SDK.
// A running Serve blocks on a listener, so this reads the source instead.
func TestThePluginCommandReachesTheGRPCEntryPoint(t *testing.T) {
	t.Parallel()
	assert.Contains(t,
		packageCalls(t, "provider", "cmd", "pulumi-resource-mailslurp", "main.go"),
		"provider.Serve", "the plugin command must call Serve")
	assert.Contains(t, packageCalls(t, "provider", "provider.go"),
		"provider.Main", "Serve must call the gRPC entry point of the Pulumi SDK")
}

// New must build the provider of the internal package and wrap it in the raw server. Without both
// links the plugin answers no schema and no resource.
func TestNewWrapsTheInternalProvider(t *testing.T) {
	t.Parallel()
	called := packageCalls(t, "provider", "provider.go")
	assert.Contains(t, called, "internal.NewProvider")
	assert.Contains(t, called, "gp.RawServer")
}
