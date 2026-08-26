package provider

// The root module pins the whole provider toolchain before the provider code exists.
// The blank imports below stop `go mod tidy` from pruning those pins.
import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/blang/semver"
	_ "github.com/pulumi/providertest"
	_ "github.com/pulumi/pulumi-go-provider"
	_ "github.com/pulumi/pulumi/pkg/v3/codegen/schema"
	_ "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	_ "go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/jschady/pulumi-mailslurp/provider/pkg/version"
)

const (
	goDirective      = "go 1.26.0"
	openAPIVersion   = "3.0.1"
	openAPIPathCount = 558
	specInfoVersion  = "6.5.2"
	versionLdflag    = "-X github.com/jschady/pulumi-mailslurp/provider/pkg/version.Version="
)

func pinnedModules() map[string]string {
	return map[string]string{
		"github.com/blang/semver":              "v3.5.1+incompatible",
		"github.com/pulumi/providertest":       "v0.7.0",
		"github.com/pulumi/pulumi-go-provider": "v1.5.0",
		"github.com/pulumi/pulumi/pkg/v3":      "v3.258.0",
		"github.com/pulumi/pulumi/sdk/v3":      "v3.258.0",
		"github.com/stretchr/testify":          "v1.11.1",
		"go.uber.org/mock":                     "v0.6.0",
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	return root
}

func runMake(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("make", args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	out, err := command.Output()
	require.NoError(t, err, "git %s", strings.Join(args, " "))
	return string(out)
}

// A file scan reads no metadata. The brand this repository never carries can still ride in the
// author of a commit, in the committer, or in a reflog entry that an amend left behind.
func TestNoBrandInTheGitMetadata(t *testing.T) {
	root := repoRoot(t)
	// The needle is built here, so this file carries no name it forbids everywhere else.
	needle := "gno" + "sis"

	identities := gitOutput(t, root, "log", "--all", "--format=%an <%ae>%n%cn <%ce>")
	require.NotEmpty(t, strings.TrimSpace(identities), "the scan read no commit")
	for _, line := range strings.Split(identities, "\n") {
		if strings.Contains(strings.ToLower(line), needle) {
			t.Errorf("the identity %q carries the brand", strings.TrimSpace(line))
		}
	}

	logs := filepath.Join(root, ".git", "logs")
	if _, err := os.Stat(logs); os.IsNotExist(err) {
		return
	}
	require.NoError(t, filepath.WalkDir(logs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		//nolint:gosec // G304: this reads the reflog of the repository the test runs in.
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(body)), needle) {
			name, _ := filepath.Rel(root, path)
			t.Errorf("%s carries the brand, and a reflog entry survives an amend", name)
		}
		return nil
	}))
}

func TestGoModPinsEveryToolchainModuleExactly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	require.NoError(t, err)
	text := string(raw)

	require.Contains(t, strings.Split(text, "\n"), goDirective)

	found := map[string]string{}
	indirect := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if _, want := pinnedModules()[fields[0]]; !want {
			continue
		}
		found[fields[0]] = fields[1]
		indirect[fields[0]] = strings.Contains(line, "// indirect")
	}

	for module, wantVersion := range pinnedModules() {
		require.Equal(t, wantVersion, found[module], "go.mod must pin %s exactly", module)
		require.False(t, indirect[module], "%s must be a direct requirement", module)
	}
}

func TestOpenAPISpecIsPinnedAndParses(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "api", "openapi.json"))
	require.NoError(t, err)

	var spec struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &spec))
	require.Equal(t, openAPIVersion, spec.OpenAPI)
	require.Equal(t, specInfoVersion, spec.Info.Version)
	require.Len(t, spec.Paths, openAPIPathCount)
}

func TestLintProseFailsOnABannedWordAndPassesWithoutIt(t *testing.T) {
	seed := filepath.Join(t.TempDir(), "seed.md")

	writeFile(t, seed, "# The seeded page\n\nYou can utilize the provider to create an inbox.\n")
	out, err := runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.Error(t, err, "lint_prose must reject a banned word: %s", out)
	require.Contains(t, out, "utilize", "lint_prose must name the word it rejected")

	writeFile(t, seed, "# The seeded page\n\nYou can use the provider to create an inbox.\n")
	out, err = runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.NoError(t, err, "lint_prose must pass once the banned word is gone: %s", out)
}

func TestLintProseRejectsTextThatNamesTheWritingStandard(t *testing.T) {
	seed := filepath.Join(t.TempDir(), "seed.md")

	// The repository must never carry the name, so the test builds the marker at run time.
	marker := strings.ToUpper("asd") + "-" + strings.ToUpper("ste") + "100"
	writeFile(t, seed, "# The seeded page\n\nThe provider follows "+marker+" rules.\n")

	out, err := runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.Error(t, err, "lint_prose must reject text that names the standard: %s", out)
	require.Contains(t, out, "seed.md", "lint_prose must name the file it rejected")
	require.NotContains(t, out, marker, "lint_prose must not print the name it rejected")
}

func TestLintProseIgnoresBannedWordsInsideCode(t *testing.T) {
	seed := filepath.Join(t.TempDir(), "seed.md")

	writeFile(t, seed, "# The seeded page\n\nRun the command:\n\n```bash\ngo run ./cmd/just-a-tool\n```\n\n"+
		"The `capabilities` property lists the functions.\n")

	out, err := runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.NoError(t, err, "a banned word inside code is a literal, so lint_prose must pass: %s", out)
}

func TestLintProseRejectsASentenceOverTheWordLimit(t *testing.T) {
	seed := filepath.Join(t.TempDir(), "seed.md")
	long := "The provider" + strings.Repeat(" and the inbox", 12) + " work together.\n"

	writeFile(t, seed, "# The seeded page\n\n"+long)
	out, err := runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.Error(t, err, "lint_prose must reject a sentence over the word limit: %s", out)
	require.Contains(t, out, "seed.md", "lint_prose must name the file it rejected")
}

func TestLintProsePassesForTheCommittedProse(t *testing.T) {
	out, err := runMake(t, "lint_prose")
	require.NoError(t, err, "the committed prose must pass lint_prose: %s", out)
}

func TestMakefileDefinesEveryTargetName(t *testing.T) {
	targets := []string{
		"ensure", "tidy", "lint", "lint_fix", "lint_prose", "provider", "generate_schema",
		"codegen", "sdk", "generate_nodejs", "generate_python", "generate_dotnet", "generate_go",
		"build_nodejs", "build_python", "build_dotnet", "build_go", "install_nodejs_sdk",
		"install_python_sdk", "install_dotnet_sdk", "install_go_sdk", "test_provider",
		"test_generated", "test_integration", "record_integration", "test_integration_live",
		"test_examples", "record_examples", "test_examples_live", "compile_examples",
		"compile_nodejs_example", "compile_python_example", "compile_dotnet_example",
		"compile_go_example", "examples", "docs", "release_snapshot", "clean",
	}

	for _, target := range targets {
		out, err := runMake(t, "-n", target)
		require.NoError(t, err, "the Makefile must define %s: %s", target, out)
		// A target name that matches a directory (provider, docs, sdk) makes an absent
		// rule look successful, so reject the reply make gives when no rule ran.
		require.NotContains(t, out, "Nothing to be done", "the Makefile must define %s", target)
	}
}

func TestProviderTargetLinksTheVersionSymbol(t *testing.T) {
	out, err := runMake(t, "-n", "provider")
	require.NoError(t, err, out)
	require.Contains(t, out, versionLdflag, "the provider target must set the one version symbol")
	require.Empty(t, version.Version, "the version symbol stays empty until the linker sets it")
}

// The recorder that reads and writes the cassettes lives under internal/. No other target builds
// that package, so a run that named the provider packages alone never ran its tests.
func TestTestProviderRunsThePackagesUnderInternal(t *testing.T) {
	out, err := runMake(t, "-n", "test_provider")
	require.NoError(t, err, out)
	require.Contains(t, out, "./provider/...", "test_provider must run the provider packages")
	require.Contains(t, out, "./internal/...", "test_provider must run the packages under internal")
}

// stubMarker opens the line the fake test command prints. The line carries the mode the recipe
// exported, so the check reads what a target does and not what it says.
const stubMarker = "the fake test command ran in mode "

// runTargetWithAStubbedTestCommand runs one target with the test command and the Pulumi CLI
// replaced, and with the key the caller names. The stub prints the mode and stops, so the run
// reaches no test, no plugin and no account.
func runTargetWithAStubbedTestCommand(t *testing.T, key, target string) (string, error) {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "stub.sh")
	writeFile(t, stub, "#!/bin/sh\necho \""+stubMarker+"[$MAILSLURP_TEST_MODE]\"\n")

	cmd := exec.Command("make", target, "GOTEST=sh "+stub, "PULUMI=true")
	cmd.Dir = repoRoot(t)
	// The last value of a name wins, so these two clear whatever the caller of the suite set.
	cmd.Env = append(os.Environ(), "MAILSLURP_API_KEY="+key, "MAILSLURP_TEST_MODE=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// A recipe that reaches MailSlurp reads the key first, and it names the mode it runs in. A recipe
// that lost the guard would reach the account with no key. A recipe that lost the mode would read
// the cassettes instead of writing them, and a recording would answer that it changed nothing.
func TestTheCredentialedTargetsReadTheKeyAndNameTheirMode(t *testing.T) {
	for _, tc := range []struct{ target, mode string }{
		{"record_integration", "record"},
		{"test_integration_live", "live"},
		{"record_examples", "record"},
		{"test_examples_live", "live"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			out, err := runTargetWithAStubbedTestCommand(t, "", tc.target)
			require.Error(t, err, "%s must stop when the key is empty: %s", tc.target, out)
			require.Contains(t, out, "MAILSLURP_API_KEY is empty",
				"%s must name the variable it needs", tc.target)
			require.NotContains(t, out, stubMarker,
				"%s must read the key before it runs the tests", tc.target)

			out, err = runTargetWithAStubbedTestCommand(t, "a-value-that-is-no-key", tc.target)
			require.NoError(t, err, out)
			require.Contains(t, out, stubMarker+"["+tc.mode+"]",
				"%s must run the tests in the %s mode", tc.target, tc.mode)
		})
	}
}

// Every pull request replays, so the two targets it runs need no key and name no mode. An unset
// mode variable is the replay mode.
func TestTheReplayTargetsNeedNoKeyAndNameNoMode(t *testing.T) {
	for _, target := range []string{"test_integration", "test_examples"} {
		t.Run(target, func(t *testing.T) {
			out, err := runTargetWithAStubbedTestCommand(t, "", target)
			require.NoError(t, err, "%s must run with no key: %s", target, out)
			require.Contains(t, out, stubMarker+"[]",
				"%s must leave the mode unset, which reads the cassettes", target)
		})
	}
}
