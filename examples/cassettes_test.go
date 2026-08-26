package examples

import (
	"testing"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
)

// The scan over the cassettes of this package. It reads files and calls nothing, so it carries no
// build tag and runs in every build.

// cassetteDir holds one cassette and one seed for each test that reaches the API. The path is
// relative to this package, which is the working directory of every test here.
const cassetteDir = "testdata/cassettes"

// A cassette that carried the API key would publish it to everyone who reads the repository.
func TestTheCassettesOfTheExamplesCarryNoCredential(t *testing.T) {
	t.Parallel()
	replaytest.RequireCleanCassettes(t, cassetteDir)
}
