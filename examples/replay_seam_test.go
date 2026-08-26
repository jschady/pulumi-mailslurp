//go:build yaml || all

package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/providertest/providers"
	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/opttest"
	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
	"github.com/jschady/pulumi-mailslurp/provider"
)

// The proof of the whole seam, from the program to the cassette. It needs no account: the
// cassette holds nothing, so the first call of the provider is one the recorder cannot answer.

// emptyCassetteBody is a cassette of the recorded format that holds no interaction.
const emptyCassetteBody = "version: 2\ninteractions: []\n"

// seamSeed is the seed this proof writes beside the cassette. A replay reads the seed of its
// recording, and this proof records nothing, so it hands the fixture one of its own.
const seamSeed = "5ea3f00d5ea3f00d5ea3f00d5ea3f00d"

// notFoundText is what the recorder reports for a call the cassette does not hold.
const notFoundText = "requested interaction not found"

// The configuration keys the base program declares. A key this list misses leaves the program
// short of a value, and the update then fails before it reaches the provider.
const (
	inboxIDKey       = "inboxId"
	webhookNameKey   = "webhookName"
	webhookURLKey    = "webhookUrl"
	rulesetTargetKey = "rulesetTarget"
	templateNameKey  = "templateName"
)

// theVendorHost answers the host of the MailSlurp API, which every call of the provider carries.
func theVendorHost() string { return strings.TrimPrefix(baseURL, "https://") }

// theEmptyCassette writes a cassette that holds no interaction, and the seed beside it, into a
// directory of this test alone. The committed cassette directory keeps nothing this proof wrote.
func theEmptyCassette(t *testing.T) *replaytest.Fixture {
	t.Helper()
	dir := t.TempDir()
	name := replaytest.CassetteName(t.Name())

	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"),
		[]byte(emptyCassetteBody), 0o600), "write the cassette of this proof")
	require.NoError(t, os.WriteFile(replaytest.SeedPath(dir, name),
		[]byte(seamSeed), 0o600), "write the seed of this proof")

	return replaytest.New(t, dir)
}

// theBaseStack builds the base program on a stack of its own and attaches the provider the
// factory answers. The stack reads no plugin from the disk, so the program can reach no other
// build of the provider.
func theBaseStack(t *testing.T, serve providers.ResourceProviderServerFactory,
	configuration map[string]string,
) *pulumitest.PulumiTest {
	t.Helper()
	// The provider reads the key from the environment, and it refuses an empty one. The harness
	// hands a replay the placeholder, which is what every leg of this package deploys with.
	requireAPIKey(t)

	pt := pulumitest.NewPulumiTest(t, filepath.Join(cwd(t), baseSource),
		opttest.AttachProviderServer(provider.Name, serve),
		opttest.SkipInstall())
	for key, value := range configuration {
		pt.SetConfig(t, key, value)
	}
	return pt
}

// theBaseConfiguration answers a value for every configuration key the base program declares. The
// names come from the seed of the fixture, which is what a replayed leg draws its names from.
func theBaseConfiguration(t *testing.T, fixture *replaytest.Fixture) map[string]string {
	t.Helper()
	webhookName := newTestName(t, fixture.Seed(), webhookKind)
	return map[string]string{
		inboxIDKey:       newTestName(t, fixture.Seed(), inboxKind),
		webhookNameKey:   webhookName,
		webhookURLKey:    webhookURLFor(webhookName),
		rulesetTargetKey: rulesetTargetFor(newTestName(t, fixture.Seed(), rulesetKind)),
		templateNameKey:  newTestName(t, fixture.Seed(), templateKind),
	}
}

// The seam runs from the program to the cassette. The update deploys the base program against a
// cassette that holds nothing, and the three answers below prove each joint of it: the update
// failed, the recorder saw a call to the API, and the failure names the call it could not answer.
// A leg that still reached the account, or a provider whose calls went around the recorder, fails
// one of them.
func TestTheSeamFromTheProgramToTheCassetteFailsOnAnEmptyCassette(t *testing.T) {
	t.Setenv(replaytest.ModeVariable, string(replaytest.Replay))
	// The run holds no key at all, which is what the pull request job holds. The harness gives the
	// stack the placeholder of a replay, so this proof sets none of its own.
	t.Setenv(apiKeyVariable, "")

	fixture := theEmptyCassette(t)
	pt := theBaseStack(t, func(providers.PulumiTest) (rpc.ResourceProviderServer, error) {
		server, err := provider.NewWith(fixture.Transport())(nil)
		if err != nil {
			return nil, err
		}
		return outlivesTheEngine{server}, nil
	}, theBaseConfiguration(t, fixture))

	_, err := pt.UpErr(t)
	require.Error(t, err, "an update that reads a cassette holding nothing should fail")

	assert.Contains(t, strings.Join(fixture.Requests(), "\n"), theVendorHost(),
		"the provider should send the calls of the program through the recorder of this test")
	assert.Contains(t, err.Error(), notFoundText,
		"the update should report the call that no cassette holds")
}
