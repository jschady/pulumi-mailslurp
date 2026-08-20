package provider

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const packageNameScript = "scripts/check-package-names.sh"

// npm and PyPI are two separate decisions that landed on the same name, so each is named here.
// A change to one must not silently move the other.
const pypiPackageName = "pulumi-mailslurp"

// The environment variables the check reads its three registries from. They exist so the decision
// the check makes about a status code can be exercised without reaching the real registries.
var packageNameRegistryVars = []string{
	"PACKAGE_CHECK_NPM_URL",
	"PACKAGE_CHECK_PYPI_URL",
	"PACKAGE_CHECK_NUGET_URL",
}

// The path each registry answers a name lookup on. npm and PyPI carry the same package name, so
// only these shapes tell the two probes apart.
func registryProbePaths() []string {
	return []string{
		"/" + npmPackageName,
		"/pypi/" + pypiPackageName + "/json",
		"/v3-flatcontainer/" + strings.ToLower(nugetPackageID) + "/index.json",
	}
}

// registryStub answers every request with one status and records the paths it was asked for.
type registryStub struct {
	server *httptest.Server
	mu     sync.Mutex
	paths  []string
}

func newRegistryStub(t *testing.T, status func(path string) int) *registryStub {
	t.Helper()
	stub := &registryStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.paths = append(stub.paths, r.URL.Path)
		stub.mu.Unlock()
		w.WriteHeader(status(r.URL.Path))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *registryStub) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func runPackageNameCheck(t *testing.T, stub *registryStub) (string, error) {
	t.Helper()
	cmd := exec.Command("./" + packageNameScript)
	cmd.Dir = repoRoot(t)
	cmd.Env = os.Environ()
	for _, name := range packageNameRegistryVars {
		cmd.Env = append(cmd.Env, name+"="+stub.server.URL)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestThePackageNameCheckFailsOnATakenName is what the gate exists for. Publishing under a name
// this repository does not hold gives the next version to whoever does.
func TestThePackageNameCheckFailsOnATakenName(t *testing.T) {
	for _, probe := range registryProbePaths() {
		t.Run(probe, func(t *testing.T) {
			stub := newRegistryStub(t, func(path string) int {
				if path == probe {
					return http.StatusOK
				}
				return http.StatusNotFound
			})

			out, err := runPackageNameCheck(t, stub)
			require.Error(t, err, "the check passed while %s was taken:\n%s", probe, out)
			assert.Contains(t, out, "already published",
				"the check must say the name is taken:\n%s", out)
		})
	}
}

// TestThePackageNameCheckFailsWhenARegistryCannotBeRead keeps a broken read out of the answer. A
// registry that could not be reached has said nothing about who holds the name.
func TestThePackageNameCheckFailsWhenARegistryCannotBeRead(t *testing.T) {
	stub := newRegistryStub(t, func(string) int { return http.StatusInternalServerError })

	out, err := runPackageNameCheck(t, stub)
	require.Error(t, err, "the check passed while no registry answered:\n%s", out)
	assert.Contains(t, out, "500", "the check must report the status it read:\n%s", out)
	assert.Contains(t, out, "unread", "the check must say the name went unread:\n%s", out)
}

// The paths the check asked for, read on the answer the first release needs. A path naming
// nothing answers 404, and 404 is the answer that reads as free.
func TestThePackageNameCheckProbesTheNamesThisRepositoryPublishes(t *testing.T) {
	stub := newRegistryStub(t, func(string) int { return http.StatusNotFound })
	out, err := runPackageNameCheck(t, stub)
	require.NoError(t, err, "the check must pass while every name is free:\n%s", out)

	assert.ElementsMatch(t, registryProbePaths(), stub.seen(),
		"each registry answers a name lookup on its own path")
}

// TestThePackageNameCheckRunsNowhereAutomatically reads the workflows. Every name belongs to this
// repository the moment the first release publishes, so a scheduled run would fail from then on.
func TestThePackageNameCheckRunsNowhereAutomatically(t *testing.T) {
	workflows := workflowFiles(t)
	require.NotEmpty(t, workflows, "the repository holds no workflow file")

	for _, workflow := range workflows {
		body, err := os.ReadFile(workflow)
		require.NoError(t, err)
		assert.NotContains(t, string(body), filepath.Base(packageNameScript),
			"%s runs %s, which fails once the names are claimed",
			filepath.Base(workflow), packageNameScript)
	}
}
