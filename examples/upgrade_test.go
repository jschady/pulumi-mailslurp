//go:build yaml || all

package examples

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/providertest"
	"github.com/pulumi/providertest/optproviderupgrade"
	"github.com/pulumi/providertest/providers"
	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertpreview"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	"github.com/jschady/pulumi-mailslurp/provider"
)

// The released version the upgrade runs from. A repository variable carries it, and it exists only
// once a release does, so an unset variable is the normal state before the first release.
const upgradeBaselineVar = "UPGRADE_BASELINE_VERSION"

// providerServerFactory serves this build inside the test process. The framework stores the host
// without reading it, and the attach path gives the server its engine connection afterwards.
func providerServerFactory(providers.PulumiTest) (rpc.ResourceProviderServer, error) {
	return provider.New(nil)
}

// upgradeBaseline answers the released version to upgrade from, and skips the caller when the
// variable is unset.
func upgradeBaseline(t *testing.T) string {
	t.Helper()
	baseline := os.Getenv(upgradeBaselineVar)
	if baseline == "" {
		t.Skipf("%s names the released provider to upgrade from and is not set", upgradeBaselineVar)
	}
	return baseline
}

// TestUpgradeFromTheReleasedProvider deploys with the released provider, then previews the same
// stack with this build. A property this build renamed becomes a change the user already owns.
func TestUpgradeFromTheReleasedProvider(t *testing.T) {
	baseline := upgradeBaseline(t)
	key := requireAPIKey(t)
	requireInboxBudget(t, upgradeProgram, 0)

	templateName := newTestName(templateKind)
	t.Cleanup(func() { sweepLeg(t, key, map[string]string{templateClass: templateName}) })

	baselineDir := installBaselinePlugin(t, baseline)

	// The harness calls SanitizeSecrets on the gRPC log unconditionally, and that call panics
	// when the log is off, so this stack cannot turn the log off the way every other leg does.
	pt := pulumitest.NewPulumiTest(t, filepath.Join(cwd(t), upgradeProgram),
		opttest.AttachProviderServer(provider.Name, providerServerFactory),
		opttest.SkipInstall(),
	)
	pt.SetConfig(t, "templateName", templateName)

	// The harness would otherwise run a plain `pulumi plugin install`, and the CLI resolves that
	// name against a registry that holds no community package. The path hands it the plugin the
	// install above downloaded.
	preview := providertest.PreviewProviderUpgrade(t, pt, provider.Name, baseline,
		optproviderupgrade.CacheDir(upgradeCacheDir(t)),
		optproviderupgrade.BaselineOpts(opttest.AttachProviderBinary(provider.Name, baselineDir)))
	assertpreview.HasNoChanges(t, preview)
}

// installBaselinePlugin downloads the released provider and answers the directory it lands in.
// The download names the server the committed schema advertises, because a plain install reads
// a registry that holds no community plugin, and the caller hands the path to the harness.
func installBaselinePlugin(t *testing.T, baseline string) string {
	t.Helper()
	//nolint:gosec // G204: the arguments are a constant, a repository variable, and the server
	// the committed schema advertises. Each reaches the CLI as one argument.
	command := exec.Command("pulumi", "plugin", "install", "resource", provider.Name, baseline,
		"--server", pluginDownloadURL(t, schemaPath))
	answered, err := command.CombinedOutput()
	require.NoErrorf(t, err, "install the %s baseline plugin: %s", baseline, answered)

	pluginDir, err := workspace.GetPluginDir()
	require.NoError(t, err, "read the plugin directory")
	installed := filepath.Join(pluginDir, "resource-"+provider.Name+"-v"+baseline)
	_, err = os.Stat(installed)
	require.NoErrorf(t, err, "the install left no plugin at %s", installed)
	return installed
}

// pluginDownloadURL reads the server a schema advertises. The committed schema is what every
// consumer of a released SDK reads, so the baseline comes from where a user's would.
func pluginDownloadURL(t *testing.T, schema string) string {
	t.Helper()
	raw, err := os.ReadFile(schema)
	require.NoError(t, err, "read the committed schema")

	var advertised struct {
		PluginDownloadURL string `json:"pluginDownloadURL"`
	}
	require.NoError(t, json.Unmarshal(raw, &advertised), "parse the committed schema")
	require.NotEmpty(t, advertised.PluginDownloadURL,
		"the schema advertises no download server, so no consumer can install the plugin")
	return advertised.PluginDownloadURL
}

// upgradeCacheDir names where the harness records the baseline. It writes an exported stack and a
// gRPC log there, both holding the API key, so the directory is a temporary one, not examples/.
func upgradeCacheDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// The harness default sits inside this package and takes a decrypted stack export, so a recorded
// run would leave the API key under examples/ for anyone who reads the working tree.
func TestTheUpgradeRecordingStaysOutOfTheRepository(t *testing.T) {
	fallback := providertest.GetUpgradeCacheDir(upgradeProgram, "0.0.0")
	require.False(t, filepath.IsAbs(fallback),
		"the harness default should be a path inside the package, and it is %s", fallback)

	chosen := upgradeCacheDir(t)
	require.True(t, filepath.IsAbs(chosen), "the recording directory should be absolute")
	assert.NotContains(t, chosen, cwd(t), "the recording directory %s sits inside %s", chosen, cwd(t))
}

// The factory is what attaches this build in process. A nil host reaches the framework, which
// stores it without reading it, and the attach path replaces it with the engine's own.
func TestTheUpgradeAttachesTheProviderThisRepositoryBuilds(t *testing.T) {
	server, err := providerServerFactory(nil)
	require.NoError(t, err, "the factory should serve this build")
	require.NotNil(t, server)
}

// The gate is what keeps a run without a released provider from failing. It skips before the
// credential is read, so a fork with no key and no release reports a skip rather than an error.
func TestTheUpgradeSkipsUntilAReleaseExists(t *testing.T) {
	for _, workflow := range []string{"pull-request.yml", "main.yml", "acceptance-tests.yml"} {
		assert.Containsf(t, readProgram(t, filepath.Join("..", ".github", "workflows", workflow)),
			upgradeBaselineVar+": ${{ vars."+upgradeBaselineVar+" }}",
			"%s must hand the credentialed job the variable a person populates", workflow)
	}

	t.Setenv(upgradeBaselineVar, "")
	var skipped bool
	t.Run("with no baseline", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		upgradeBaseline(t)
	})
	assert.True(t, skipped, "an unset variable should skip the upgrade rather than fail it")

	t.Setenv(upgradeBaselineVar, "0.1.0")
	assert.Equal(t, "0.1.0", upgradeBaseline(t), "a set variable names the release to upgrade from")
}

// The baseline plugin comes from the server the schema advertises, and no other. A plugin
// installed from anywhere else is not the artifact a user upgrading from the release runs.
func TestTheBaselineInstallReadsTheServerTheSchemaAdvertises(t *testing.T) {
	// A schema nobody committed, so an answer that matches it can only have been read from it.
	elsewhere := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, os.WriteFile(elsewhere,
		[]byte(`{"pluginDownloadURL": "github://api.example.invalid/somebody/pulumi-elsewhere"}`), 0o600))
	assert.Equal(t, "github://api.example.invalid/somebody/pulumi-elsewhere",
		pluginDownloadURL(t, elsewhere), "the server comes from the schema it is given")
}
