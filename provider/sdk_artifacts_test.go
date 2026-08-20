package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sdkArtifactScript = "scripts/check-sdk-artifacts.sh"
	nugetPackageID    = "Jschady.Mailslurp"
)

// sdkArtifactFixture lays out the publishable artifact of each SDK the way the release job does
// after it restores the four build artifacts. The Go SDK publishes by tag, so its module is it.
func sdkArtifactFixture(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
	write("nodejs/bin/package.json", `{"name": "`+npmPackageName+`", "version": "`+version+`"}`)
	write("python/bin/dist/"+pythonModuleName+"-"+version+"-py3-none-any.whl", "wheel")
	write("python/bin/dist/"+pythonModuleName+"-"+version+".tar.gz", "sdist")
	write("dotnet/bin/Debug/"+nugetPackageID+"."+version+".nupkg", "package")
	write("go/mailslurp/go.mod", "module "+goImportBasePath+"\n")
	return dir
}

func runSDKArtifactCheck(t *testing.T, dir, version string) (string, error) {
	t.Helper()
	cmd := exec.Command("./"+sdkArtifactScript, dir, version)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func rewriteFixtureFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600))
}

func renameFixtureFile(t *testing.T, dir, from, to string) {
	t.Helper()
	require.NoError(t, os.Rename(filepath.Join(dir, from), filepath.Join(dir, to)))
}

// TestTheSDKArtifactCheckPassesOnACompleteSet is the state a release needs before one binary
// reaches the release page. Each language reports the artifact it would publish.
func TestTheSDKArtifactCheckPassesOnACompleteSet(t *testing.T) {
	out, err := runSDKArtifactCheck(t, sdkArtifactFixture(t, "0.1.0"), "v0.1.0")
	require.NoError(t, err, "the check must pass on a complete set:\n%s", out)
	for _, language := range sdkLanguages() {
		assert.Contains(t, out, "ok: "+language, "the check must report what %s publishes:\n%s", language, out)
	}
}

// TestTheSDKArtifactCheckAcceptsAPrereleaseSpelling covers the versions that differ per language.
// A Python wheel spells a prerelease 0.1.0a1 and the tag spells the same release 0.1.0-alpha.1.
func TestTheSDKArtifactCheckAcceptsAPrereleaseSpelling(t *testing.T) {
	dir := sdkArtifactFixture(t, "0.1.0-alpha.1")
	renameFixtureFile(t, dir,
		filepath.Join("python", "bin", "dist", pythonModuleName+"-0.1.0-alpha.1-py3-none-any.whl"),
		filepath.Join("python", "bin", "dist", pythonModuleName+"-0.1.0a1-py3-none-any.whl"))
	renameFixtureFile(t, dir,
		filepath.Join("python", "bin", "dist", pythonModuleName+"-0.1.0-alpha.1.tar.gz"),
		filepath.Join("python", "bin", "dist", pythonModuleName+"-0.1.0a1.tar.gz"))

	out, err := runSDKArtifactCheck(t, dir, "v0.1.0-alpha.1")
	require.NoError(t, err, "the check must pass on a prerelease set:\n%s", out)
}

// stalePrereleaseArtifacts rewrites one language's staged artifact into the prerelease spelling it
// carried at the previous build, leaving the other three at the release the tag names.
func stalePrereleaseArtifacts() map[string]func(t *testing.T, dir string) {
	dist := filepath.Join("python", "bin", "dist")
	debug := filepath.Join("dotnet", "bin", "Debug")
	return map[string]func(t *testing.T, dir string){
		nodejsLanguage: func(t *testing.T, dir string) {
			rewriteFixtureFile(t, dir, filepath.Join("nodejs", "bin", "package.json"),
				`{"name": "`+npmPackageName+`", "version": "0.1.0-alpha.1"}`)
		},
		pythonLanguage: func(t *testing.T, dir string) {
			renameFixtureFile(t, dir,
				filepath.Join(dist, pythonModuleName+"-0.1.0-py3-none-any.whl"),
				filepath.Join(dist, pythonModuleName+"-0.1.0a1-py3-none-any.whl"))
		},
		dotnetLanguage: func(t *testing.T, dir string) {
			renameFixtureFile(t, dir,
				filepath.Join(debug, nugetPackageID+".0.1.0.nupkg"),
				filepath.Join(debug, nugetPackageID+".0.1.0-alpha.1.nupkg"))
		},
	}
}

// A final tag must publish final artifacts. The version compare read a prefix, so a stale
// prerelease build answered for the release and the release page would carry it.
func TestTheSDKArtifactCheckRefusesAPrereleaseArtifactForAFinalTag(t *testing.T) {
	for language, makeStale := range stalePrereleaseArtifacts() {
		t.Run(language, func(t *testing.T) {
			dir := sdkArtifactFixture(t, "0.1.0")
			makeStale(t, dir)

			out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
			require.Errorf(t, err, "the check passed on a %s prerelease artifact under a final tag:\n%s",
				language, out)
			assert.Containsf(t, out, language, "the check must name the stale SDK:\n%s", out)
		})
	}
}

// The upload carries the wheel and the source distribution, and the check read the version of the
// wheel alone. A stale source distribution beside a fresh wheel publishes under the release tag.
func TestTheSDKArtifactCheckRefusesAStaleSourceDistribution(t *testing.T) {
	dist := filepath.Join("python", "bin", "dist")
	dir := sdkArtifactFixture(t, "0.1.0")
	renameFixtureFile(t, dir, filepath.Join(dist, pythonModuleName+"-0.1.0.tar.gz"),
		filepath.Join(dist, pythonModuleName+"-0.1.0a1.tar.gz"))

	out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
	require.Errorf(t, err, "the check passed on a prerelease source distribution under a final tag:\n%s", out)
	assert.Containsf(t, out, "python", "the check must name the stale SDK:\n%s", out)
}

// TestTheSDKArtifactCheckFailsOnAMissingLanguage is what the gate exists for. Binaries that
// publish while one SDK is missing leave a release nobody can consume from that language.
func TestTheSDKArtifactCheckFailsOnAMissingLanguage(t *testing.T) {
	for _, language := range sdkLanguages() {
		t.Run(language, func(t *testing.T) {
			dir := sdkArtifactFixture(t, "0.1.0")
			require.NoError(t, os.RemoveAll(filepath.Join(dir, language)))

			out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
			require.Error(t, err, "the check passed with no %s SDK:\n%s", language, out)
			assert.Contains(t, out, language, "the check must name the missing SDK:\n%s", out)
		})
	}
}

// TestTheSDKArtifactCheckFailsOnADisagreeingVersion is the defect the .NET package carried once:
// the project declared no version, so every build packed the tool default of 1.0.0.
func TestTheSDKArtifactCheckFailsOnADisagreeingVersion(t *testing.T) {
	misbuilds := map[string]func(t *testing.T, dir string){
		nodejsLanguage: func(t *testing.T, dir string) {
			rewriteFixtureFile(t, dir, filepath.Join("nodejs", "bin", "package.json"),
				`{"name": "`+npmPackageName+`", "version": "1.0.0"}`)
		},
		pythonLanguage: func(t *testing.T, dir string) {
			dist := filepath.Join("python", "bin", "dist")
			renameFixtureFile(t, dir,
				filepath.Join(dist, pythonModuleName+"-0.1.0-py3-none-any.whl"),
				filepath.Join(dist, pythonModuleName+"-1.0.0-py3-none-any.whl"))
		},
		dotnetLanguage: func(t *testing.T, dir string) {
			debug := filepath.Join("dotnet", "bin", "Debug")
			renameFixtureFile(t, dir,
				filepath.Join(debug, nugetPackageID+".0.1.0.nupkg"),
				filepath.Join(debug, nugetPackageID+".1.0.0.nupkg"))
		},
	}
	// The Go SDK is absent here on purpose: the module proxy reads its version from the tag, so
	// the tree carries no version of its own to disagree with the release.
	for language, misbuild := range misbuilds {
		t.Run(language, func(t *testing.T) {
			dir := sdkArtifactFixture(t, "0.1.0")
			misbuild(t, dir)

			out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
			require.Error(t, err, "the check passed on a %s artifact built at 1.0.0:\n%s", language, out)
			assert.Contains(t, out, "1.0.0", "the check must report the version it read:\n%s", out)
			assert.Contains(t, out, "0.1.0", "the check must report the release it compared:\n%s", out)
		})
	}
}

// TestTheSDKArtifactCheckFailsOnAForeignPackageName reads the identity of every language, not the
// layout. A tree carrying somebody else's name publishes this release to somebody else's package.
func TestTheSDKArtifactCheckFailsOnAForeignPackageName(t *testing.T) {
	corruptions := map[string]struct {
		corrupt func(t *testing.T, dir string)
		names   string
		// says is the refusal the name check writes. The version check refuses the same artifact,
		// so without this the name check could be deleted and every case here would still fail.
		says string
	}{
		nodejsLanguage: {
			corrupt: func(t *testing.T, dir string) {
				rewriteFixtureFile(t, dir, filepath.Join("nodejs", "bin", "package.json"),
					`{"name": "@jschady/mailslurp", "version": "0.1.0"}`)
			},
			names: npmPackageName,
			says:  "this repository publishes",
		},
		pythonLanguage: {
			corrupt: func(t *testing.T, dir string) {
				dist := filepath.Join("python", "bin", "dist")
				renameFixtureFile(t, dir,
					filepath.Join(dist, pythonModuleName+"-0.1.0-py3-none-any.whl"),
					filepath.Join(dist, "jschady_mailslurp-0.1.0-py3-none-any.whl"))
			},
			names: "jschady_mailslurp",
			says:  "is not this repository's distribution",
		},
		dotnetLanguage: {
			corrupt: func(t *testing.T, dir string) {
				debug := filepath.Join("dotnet", "bin", "Debug")
				renameFixtureFile(t, dir,
					filepath.Join(debug, nugetPackageID+".0.1.0.nupkg"),
					filepath.Join(debug, "Pulumi.Mailslurp.0.1.0.nupkg"))
			},
			names: "Pulumi.Mailslurp",
			says:  "is not this repository's package",
		},
		goLanguage: {
			corrupt: func(t *testing.T, dir string) {
				rewriteFixtureFile(t, dir, filepath.Join("go", "mailslurp", "go.mod"),
					"module github.com/pulumi/pulumi-mailslurp/sdk/go/mailslurp\n")
			},
			names: "github.com/pulumi/pulumi-mailslurp/sdk/go/mailslurp",
			says:  "the go SDK declares module",
		},
	}
	for language, corruption := range corruptions {
		t.Run(language, func(t *testing.T) {
			dir := sdkArtifactFixture(t, "0.1.0")
			corruption.corrupt(t, dir)

			out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
			require.Error(t, err, "the check passed on a %s artifact naming another package:\n%s",
				language, out)
			assert.Contains(t, out, corruption.names,
				"the check must name the identity it read:\n%s", out)
			assert.Contains(t, out, corruption.says,
				"the check must refuse the name rather than only the version:\n%s", out)
		})
	}
}

// TestTheSDKArtifactCheckFailsOnASourceDistributionWithoutAWheel is the other half of the Python
// upload. A release shipping no wheel makes every install build from source.
func TestTheSDKArtifactCheckFailsOnASourceDistributionWithoutAWheel(t *testing.T) {
	dir := sdkArtifactFixture(t, "0.1.0")
	require.NoError(t, os.Remove(filepath.Join(dir, "python", "bin", "dist",
		pythonModuleName+"-0.1.0-py3-none-any.whl")))

	out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
	require.Error(t, err, "the check passed with a source distribution and no wheel:\n%s", out)
	assert.Contains(t, out, "no wheel", "the check must name the missing wheel:\n%s", out)
}

// TestTheSDKArtifactCheckFailsOnAWheelWithoutASourceDistribution covers the second file the Python
// upload carries. A release shipping no source distribution cannot be built from source.
func TestTheSDKArtifactCheckFailsOnAWheelWithoutASourceDistribution(t *testing.T) {
	dir := sdkArtifactFixture(t, "0.1.0")
	require.NoError(t, os.Remove(filepath.Join(dir, "python", "bin", "dist",
		pythonModuleName+"-0.1.0.tar.gz")))

	out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
	require.Error(t, err, "the check passed with a wheel and no source distribution:\n%s", out)
	assert.Contains(t, out, "source distribution",
		"the check must name the missing source distribution:\n%s", out)
}

// TestTheSDKArtifactCheckReportsEveryProblemAtOnce reads the whole set before it refuses. A check
// that stopped at the first language would hide the other three behind a repeated release run.
func TestTheSDKArtifactCheckReportsEveryProblemAtOnce(t *testing.T) {
	dir := t.TempDir()
	out, err := runSDKArtifactCheck(t, dir, "v0.1.0")
	require.Error(t, err, "the check passed on an empty set:\n%s", out)
	for _, language := range sdkLanguages() {
		assert.Contains(t, out, language, "the check must name %s:\n%s", language, out)
	}
}

// TestTheSDKArtifactCheckRefusesAnIncompleteCall names its own arguments. A release job that
// called it with one argument would otherwise read a directory named by the empty string.
func TestTheSDKArtifactCheckRefusesAnIncompleteCall(t *testing.T) {
	//nolint:gosec // G204: the argument is a directory this test just made under TempDir.
	cmd := exec.Command("./"+sdkArtifactScript, sdkArtifactFixture(t, "0.1.0"))
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "the check ran with no version:\n%s", out)
	assert.Contains(t, string(out), "<version>", "the check must name the arguments it takes")
}

// stagedVersionProbe is a release version no generator writes, so a staged file carrying it can
// only have been stamped by the build.
const stagedVersionProbe = "9.9.9"

// stagedPythonVersionCommand answers the command the python build runs over pyproject.toml, read
// out of the expanded recipe so the test exercises what the build really runs.
func stagedPythonVersionCommand(t *testing.T, recipe string) string {
	t.Helper()
	for _, line := range strings.Split(recipe, "\n") {
		command, found := strings.CutSuffix(strings.TrimSpace(line), "> bin/pyproject.toml && \\")
		if found {
			return strings.TrimSpace(command)
		}
	}
	t.Fatal("the python build stages no copy of pyproject.toml")
	return ""
}

// The generator writes pyproject.toml with a placeholder version, and the build stamps the
// release into the staged copy. An indent the generator changes must not publish 0.0.0.
func TestTheStagedPythonProjectTakesTheVersionAtAnyIndent(t *testing.T) {
	recipe, err := runMake(t, "-n", "build_python", "PROVIDER_VERSION="+stagedVersionProbe)
	require.NoError(t, err, recipe)
	command := stagedPythonVersionCommand(t, recipe)

	for name, indent := range map[string]string{"flush": "", "two spaces": "  ", "four spaces": "    "} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"),
				[]byte("[project]\n"+indent+`name = "`+pythonModuleName+"\"\n"+
					indent+`version = "0.0.0"`+"\n"), 0o600))

			staged := exec.Command("bash", "-c", command)
			staged.Dir = dir
			answered, err := staged.CombinedOutput()
			require.NoError(t, err, string(answered))

			assert.Contains(t, string(answered), `version = "`+stagedVersionProbe+`"`,
				"the staged project must carry the release version")
			assert.NotContains(t, string(answered), `"0.0.0"`,
				"the staged project still carries the placeholder, so the wheel would publish at 0.0.0")
		})
	}
}

// TestNoBinaryPublishesBeforeTheSDKSetIsConfirmed walks the release job graph. A binary on the
// release page that no SDK can be published against is a release nobody can consume.
func TestNoBinaryPublishesBeforeTheSDKSetIsConfirmed(t *testing.T) {
	jobs := releaseJobs(t)

	var gate string
	for name, block := range jobs {
		if strings.Contains(block, sdkArtifactScript) {
			gate = name
		}
	}
	require.NotEmpty(t, gate, "no release job runs %s, so nothing confirms the SDK set", sdkArtifactScript)

	require.Contains(t, jobs, "publish_provider")
	assert.True(t, jobReaches(jobs, "publish_provider", gate, map[string]bool{}),
		"publish_provider needs %v and none of them reaches %s",
		jobNeeds(jobs["publish_provider"]), gate)
}

// releaseJobs answers the body of every job in release.yml, keyed by identifier.
func releaseJobs(t *testing.T) map[string]string {
	t.Helper()
	jobs := map[string]string{}
	for _, name := range workflowJobs(t, releaseWorkflow) {
		jobs[name] = workflowJobBody(t, releaseWorkflow, name)
	}
	return jobs
}

// jobReaches reports whether one job depends on another, directly or through its own needs.
func jobReaches(jobs map[string]string, from, target string, seen map[string]bool) bool {
	if seen[from] {
		return false
	}
	seen[from] = true
	for _, need := range jobNeeds(jobs[from]) {
		if need == target || jobReaches(jobs, need, target, seen) {
			return true
		}
	}
	return false
}

// jobNeeds reads the jobs one job waits for, in either the inline or the list spelling.
func jobNeeds(block string) []string {
	var needs []string
	inList := false
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "needs:":
			inList = true
		case strings.HasPrefix(trimmed, "needs: ["):
			for _, name := range strings.Split(strings.Trim(strings.TrimPrefix(trimmed, "needs: ["), "]"), ",") {
				needs = append(needs, strings.TrimSpace(name))
			}
		case strings.HasPrefix(trimmed, "needs: "):
			needs = append(needs, strings.TrimPrefix(trimmed, "needs: "))
		case inList && strings.HasPrefix(trimmed, "- "):
			needs = append(needs, strings.TrimPrefix(trimmed, "- "))
		default:
			inList = false
		}
	}
	return needs
}
