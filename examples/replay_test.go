//go:build yaml || nodejs || python || dotnet || go || all

package examples

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
)

// The recorder of one leg. A test that reaches the API builds one before it builds anything else,
// so the calls of the provider and the calls of this package land in one cassette.

// recorderFor answers the fixture of the calling test. Replay reads the cassette of that test and
// fails when it is missing, record writes one, and live sends every call on and writes none.
func recorderFor(t *testing.T) *replaytest.Fixture {
	t.Helper()
	return replaytest.New(t, cassetteDir)
}

// A replay and a record deploy the provider this test process serves, and the recorder answers
// every call it makes. A leg that still read the installed plugin would reach the account.
func TestAReplayedLegAttachesTheProviderThisProcessServes(t *testing.T) {
	t.Setenv(replaytest.ModeVariable, string(replaytest.Record))
	// The cassette of this test goes to a directory of its own, because this test deploys no
	// program and the cassette it writes holds nothing.
	fixture := replaytest.New(t, t.TempDir())

	environment := theProviderOfThisRun(t, fixture, cwd(t))
	require.Len(t, environment, 1, "a record run reads the key of the account from the environment")
	assert.Regexp(t, `^PULUMI_DEBUG_PROVIDERS=mailslurp:[0-9]+$`, environment[0],
		"the leg should point the CLI at the port this process serves")
	assert.Empty(t, baseOptions(t).LocalProviders,
		"a recorded leg attaches the provider, so it reads no plugin from the disk")
}

// A live leg runs the plugin the install target put in the Pulumi home, which is what a user runs.
func TestALiveLegRunsTheInstalledPlugin(t *testing.T) {
	t.Setenv(replaytest.ModeVariable, string(replaytest.Live))
	assert.Nil(t, theProviderOfThisRun(t, nil, cwd(t)),
		"a live leg starts no provider inside the test process")
	assert.Len(t, baseOptions(t).LocalProviders, 1,
		"a live leg reads the provider from the binary this repository built")
}
