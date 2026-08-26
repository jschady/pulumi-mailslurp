package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
)

// The scan over the cassettes of the integration tests. It reads files and calls nothing, so it
// carries no build tag and every run of the provider tests reads these cassettes back.

// integrationCassettes holds one cassette and one seed for each integration test that reaches the
// API. The path is relative to this package, which is the working directory of a test here.
const integrationCassettes = "testdata/cassettes"

// A cassette that carried the API key would publish it to everyone who reads the repository.
func TestTheCassettesOfTheIntegrationTestsCarryNoCredential(t *testing.T) {
	t.Parallel()
	replaytest.RequireCleanCassettes(t, integrationCassettes)
}

// leakedCassette is what a broken scrub writes: the credential header rides the request, and the
// body names a secret member the account issued.
const leakedCassette = `---
version: 2
interactions:
    - id: 0
      request:
        headers:
            X-Api-Key:
                - fake-key-00000000-0000-4000-8000-000000000000
        url: https://api.mailslurp.com/inboxes
        method: GET
      response:
        body: '{"apiKey":"opaque-value"}'
        code: 200
`

// The check above reads whatever the directory holds, and nobody has recorded yet, so a scan that
// read nothing at all would pass it too. This plants a cassette that carries a credential in a
// directory of its own, and reads it with the scan the check above runs.
func TestTheScanOfTheseCassettesReadsAPlantedCredential(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	planted := filepath.Join(dir, "TestPlanted.yaml")
	require.NoError(t, os.WriteFile(planted, []byte(leakedCassette), 0o600))

	findings, err := replaytest.CassetteFindings(dir)
	require.NoError(t, err)
	require.NotEmpty(t, findings, "the scan must read a cassette that carries a credential")
	require.Contains(t, findings[0], planted, "the finding must name the cassette")
}
