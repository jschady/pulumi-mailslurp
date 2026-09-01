package provider

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One tool version lives in several files that no build step derives from each other. A raise that
// reaches one copy and leaves the rest builds against one version and tests against another. The
// tests below read every copy and compare them.

// pinsToolVersion answers the version .tool-versions names for one tool.
func pinsToolVersion(t *testing.T, tool string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^` + tool + ` +(\S+)\s*$`)
	found := pattern.FindStringSubmatch(readRepoFile(t, ".tool-versions"))
	require.NotNilf(t, found, ".tool-versions names no version for %s", tool)
	return found[1]
}

// pinsWorkflowEnv answers the value each workflow gives one environment variable, keyed by the
// file name. A workflow that names the variable nowhere is absent from the answer.
func pinsWorkflowEnv(t *testing.T, name string) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	var entries []string
	// GitHub runs a workflow spelled .yaml as readily as one spelled .yml.
	for _, spelling := range []string{"*.yml", "*.yaml"} {
		matched, err := filepath.Glob(filepath.Join(dir, spelling))
		require.NoError(t, err)
		entries = append(entries, matched...)
	}
	require.NotEmpty(t, entries, "the workflow directory holds no workflow")

	pattern := regexp.MustCompile(`(?m)^\s+` + name + `:\s*(\S+)\s*$`)
	found := map[string]string{}
	for _, entry := range entries {
		//nolint:gosec // G304: the path comes from a glob over the workflow directory of this repository.
		raw, err := os.ReadFile(entry)
		require.NoError(t, err)
		matches := pattern.FindAllStringSubmatch(string(raw), -1)
		if len(matches) == 0 {
			continue
		}
		file := filepath.Base(entry)
		for _, match := range matches {
			require.Equalf(t, matches[0][1], match[1], "%s gives %s two values", file, name)
		}
		found[file] = matches[0][1]
	}
	return found
}

// pinsGoModRequire answers the version one go.mod requires for a module, without the leading `v`.
func pinsGoModRequire(t *testing.T, goMod, module string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + ` +(\S+)`)
	found := pattern.FindStringSubmatch(readRepoFile(t, goMod))
	require.NotNilf(t, found, "%s requires no %s", goMod, module)
	return strings.TrimPrefix(found[1], "v")
}

// pinsGoDirective answers the language version one go.mod declares.
func pinsGoDirective(t *testing.T, goMod string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^go +(\S+)\s*$`)
	found := pattern.FindStringSubmatch(readRepoFile(t, goMod))
	require.NotNilf(t, found, "%s declares no go directive", goMod)
	return found[1]
}

// pinsGoModules answers every go.mod the repository tracks, as a path under the repository root.
// The list comes from the tree, so a module the repository gains joins the tests below without an
// edit. The repository ignores `node_modules`, so no vendored copy reaches the list.
func pinsGoModules(t *testing.T) []string {
	t.Helper()
	listed := gitOutput(t, repoRoot(t), "ls-files", "--", "go.mod", "*/go.mod")
	var modules []string
	for _, line := range strings.Split(listed, "\n") {
		if path := strings.TrimSpace(line); path != "" {
			modules = append(modules, path)
		}
	}
	require.NotEmpty(t, modules, "the repository tracks no go.mod")
	return modules
}

// TestOnePulumiVersionAcrossEveryPin reads the Pulumi version out of every file that carries it.
// The CLI a run installs and the SDK a program links must be the same release.
func TestOnePulumiVersionAcrossEveryPin(t *testing.T) {
	t.Parallel()
	const (
		engine = "github.com/pulumi/pulumi/pkg/v3"
		sdk    = "github.com/pulumi/pulumi/sdk/v3"
	)
	want := pinsToolVersion(t, "pulumi")
	filesRead := []string{".tool-versions"}

	got := map[string]string{}
	for file, value := range pinsWorkflowEnv(t, "PULUMI_VERSION") {
		where := filepath.Join(".github", "workflows", file)
		got[where] = strings.TrimPrefix(value, "v")
		filesRead = append(filesRead, where)
	}
	require.NotEmpty(t, got, "no workflow defines PULUMI_VERSION")

	// Every module links the Pulumi SDK. The provider module links the engine as well and the
	// other modules do not, so the test reads the engine out of the modules that name it.
	names := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(engine) + ` `)
	for _, goMod := range pinsGoModules(t) {
		filesRead = append(filesRead, goMod)
		got[goMod+" "+sdk] = pinsGoModRequire(t, goMod, sdk)
		if names.MatchString(readRepoFile(t, goMod)) {
			got[goMod+" "+engine] = pinsGoModRequire(t, goMod, engine)
		}
	}
	sort.Strings(filesRead)

	for where, value := range got {
		assert.Equalf(t, want, value,
			"%s pins the Pulumi version at %s and .tool-versions names %s. The test read %s",
			where, value, want, strings.Join(filesRead, ", "))
	}
}

// TestOnePulumictlVersionAcrossEveryPin reads the pulumictl version out of every file that carries
// it. The workflows write the version with a leading `v` and .tool-versions writes it without one.
func TestOnePulumictlVersionAcrossEveryPin(t *testing.T) {
	t.Parallel()
	want := pinsToolVersion(t, "pulumictl")

	workflows := pinsWorkflowEnv(t, "PULUMICTL_VERSION")
	require.NotEmpty(t, workflows, "no workflow defines PULUMICTL_VERSION")

	filesRead := []string{".tool-versions"}
	for file := range workflows {
		filesRead = append(filesRead, filepath.Join(".github", "workflows", file))
	}
	sort.Strings(filesRead)

	for file, value := range workflows {
		assert.Equalf(t, want, strings.TrimPrefix(value, "v"),
			"%s pins pulumictl at %s and .tool-versions names %s. The test read %s",
			filepath.Join(".github", "workflows", file), value, want, strings.Join(filesRead, ", "))
	}
}

// TestOneGoDirectiveAcrossEveryModule reads the go directive of every module and the toolchain
// .tool-versions names. A module that asks for a later language than the toolchain does not build.
func TestOneGoDirectiveAcrossEveryModule(t *testing.T) {
	t.Parallel()
	modules := pinsGoModules(t)
	filesRead := strings.Join(modules, ", ")
	want := pinsGoDirective(t, "go.mod")

	for _, goMod := range modules {
		directive := pinsGoDirective(t, goMod)
		assert.Equalf(t, want, directive,
			"%s declares go %s and go.mod declares go %s. The test read %s",
			goMod, directive, want, filesRead)
	}

	// The directive names a language version and .tool-versions names a toolchain release, so the
	// two share the major and the minor and part on the patch.
	minorOf := func(version string) string {
		parts := strings.Split(version, ".")
		require.GreaterOrEqualf(t, len(parts), 2, "the version %s carries no minor", version)
		return parts[0] + "." + parts[1]
	}
	golang := pinsToolVersion(t, "golang")
	assert.Equalf(t, minorOf(want), minorOf(golang),
		".tool-versions names golang %s and go.mod declares go %s. The test read %s",
		golang, want, filesRead+", .tool-versions")
}
