package provider

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The four committed SDK trees, and the manifest each package manager reads out of them. These
// tags are the ones several separate lists share, so each has one home.
const (
	nodejsLanguage = "nodejs"
	pythonLanguage = "python"
	dotnetLanguage = "dotnet"
	goLanguage     = "go"
)

// sdkLanguages are the four languages the generator and the make targets name.
func sdkLanguages() []string {
	return []string{nodejsLanguage, pythonLanguage, dotnetLanguage, goLanguage}
}

const (
	nodejsManifest = "sdk/nodejs/package.json"
	pythonManifest = "sdk/python/pyproject.toml"
	pythonPackage  = "sdk/python/pulumi_mailslurp"
	dotnetProject  = "sdk/dotnet/Jschady.Mailslurp.csproj"
	dotnetIconPath = "sdk/dotnet/logo.png"
	goModulePath   = "sdk/go/mailslurp/go.mod"
)

// A distribution name treats runs of "-", "_" and "." as one hyphen, so pulumi_mailslurp and
// pulumi-mailslurp name the same package. https://peps.python.org/pep-0503/#normalized-names
var pypiSeparators = regexp.MustCompile(`[-_.]+`)

// firstSubmatch reads one value out of an SDK manifest. A manifest that stopped carrying the
// field would otherwise compare equal to an empty string.
func firstSubmatch(t *testing.T, rel string, pattern *regexp.Regexp) string {
	t.Helper()
	found := pattern.FindStringSubmatch(string(readRepoBytes(t, rel)))
	require.NotNil(t, found, "%s carries nothing matching %s", rel, pattern)
	return found[1]
}

// TestEachSDKManifestCarriesItsPublishedName reads the four manifests a package manager reads.
// These are the names a user installs, and no other file in the SDK states them.
func TestEachSDKManifestCarriesItsPublishedName(t *testing.T) {
	t.Parallel()
	npm := firstSubmatch(t, nodejsManifest, regexp.MustCompile(`"name":\s*"([^"]+)"`))
	assert.Equal(t, npmPackageName, npm, "the npm manifest names another package")

	pypi := firstSubmatch(t, pythonManifest, regexp.MustCompile(`(?m)^\s*name\s*=\s*"([^"]+)"`))
	assert.Equal(t, "pulumi-mailslurp", strings.ToLower(pypiSeparators.ReplaceAllString(pypi, "-")),
		"the Python manifest names the distribution %q", pypi)
	assert.DirExists(t, filepath.Join(repoRoot(t), pythonPackage),
		"a program imports the package by this directory name")

	namespace := firstSubmatch(t, "sdk/dotnet/Provider.cs", regexp.MustCompile(`(?m)^namespace\s+(\S+)`))
	assert.Equal(t, "Jschady.Mailslurp", namespace, "the .NET sources declare another namespace")
	assert.FileExists(t, filepath.Join(repoRoot(t), dotnetProject),
		"the NuGet package id is the name of the project file")

	module := firstSubmatch(t, goModulePath, regexp.MustCompile(`(?m)^module\s+(\S+)`))
	assert.Equal(t, goImportBasePath, module,
		"the Go SDK module is not the path the schema tells programs to import")
}

// TestTheMakefileNamesTheSamePackagesAsTheSDKs reads the names the release recipes will publish
// under. They are declared apart from the schema, so nothing else notices when the two disagree.
func TestTheMakefileNamesTheSamePackagesAsTheSDKs(t *testing.T) {
	t.Parallel()
	makefile := string(readRepoBytes(t, "Makefile"))
	for variable, want := range map[string]string{
		"NODE_MODULE_NAME": npmPackageName,
		"NUGET_PKG_NAME":   "Jschady.Mailslurp",
		"PYTHON_PKG_NAME":  pythonModuleName,
	} {
		pattern := regexp.MustCompile(`(?m)^` + variable + `\s*:?=\s*(\S+)`)
		found := pattern.FindStringSubmatch(makefile)
		require.NotNil(t, found, "the Makefile declares no %s", variable)
		assert.Equal(t, want, found[1], "the Makefile's %s is not the published name", variable)
	}
}

// pluginManifest is the file that tells the CLI which plugin an SDK needs and where to get it.
type pluginManifest struct {
	Resource bool   `json:"resource"`
	Name     string `json:"name"`
	Server   string `json:"server"`
	Version  string `json:"version"`
}

// pluginManifests answers the manifest each language ships, keyed by the file it lives in.
func pluginManifests(t *testing.T) map[string]pluginManifest {
	t.Helper()
	out := map[string]pluginManifest{}
	for _, rel := range []string{
		filepath.Join(pythonPackage, "pulumi-plugin.json"),
		"sdk/dotnet/pulumi-plugin.json",
		"sdk/go/mailslurp/pulumi-plugin.json",
	} {
		var found pluginManifest
		require.NoError(t, json.Unmarshal(readRepoBytes(t, rel), &found), "parse %s", rel)
		out[rel] = found
	}

	var manifest struct {
		Pulumi pluginManifest `json:"pulumi"`
	}
	require.NoError(t, json.Unmarshal(readRepoBytes(t, nodejsManifest), &manifest))
	out[nodejsManifest] = manifest.Pulumi
	return out
}

// This provider is not on the Pulumi registry, so the CLI has nothing to resolve `mailslurp`
// against. The download URL inside these manifests is the only thing that reaches the binary.
func TestEverySDKTellsTheCLIWhereToFetchThePlugin(t *testing.T) {
	t.Parallel()
	var schema struct {
		Name              string `json:"name"`
		PluginDownloadURL string `json:"pluginDownloadURL"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &schema))
	require.NotEmpty(t, schema.PluginDownloadURL, "the schema names no plugin download URL")

	manifests := pluginManifests(t)
	require.Len(t, manifests, len(sdkLanguages()), "one language ships no plugin manifest")
	for rel, manifest := range manifests {
		assert.True(t, manifest.Resource, "%s does not declare a resource plugin", rel)
		assert.Equal(t, schema.Name, manifest.Name, "%s names another plugin", rel)
		assert.Equal(t, schema.PluginDownloadURL, manifest.Server,
			"%s gives the CLI no way to acquire a plugin outside the registry", rel)
	}
}

// The committed schema carries no version, so every generated manifest holds a placeholder and
// a regeneration never depends on the build version. The release stamps the version it publishes.
func TestTheCommittedSDKSourcesCarryNoBuildVersion(t *testing.T) {
	t.Parallel()
	npm := firstSubmatch(t, nodejsManifest, regexp.MustCompile(`"version":\s*"([^"]+)"`))
	assert.Equal(t, "${VERSION}", npm, "the npm manifest carries a build version")

	pypi := firstSubmatch(t, pythonManifest, regexp.MustCompile(`(?m)^\s*version\s*=\s*"([^"]+)"`))
	assert.Equal(t, "0.0.0", pypi, "the Python manifest carries a build version")

	assert.NotContains(t, string(readRepoBytes(t, dotnetProject)), "<Version>",
		"%s declares a Version element and the build passes the version instead", dotnetProject)

	for rel, manifest := range pluginManifests(t) {
		assert.Empty(t, manifest.Version,
			"%s pins the plugin version, so the SDK stops matching its provider", rel)
	}
}

// selfReference is how one generated file spells the `groups` property of the one type in this
// schema that holds a list of itself.
type selfReference struct{ path, property string }

// recursiveType answers that property in each language. A generator that cannot express the
// cycle drops the property or emits a type that does not compile.
func recursiveType() []selfReference {
	return []selfReference{
		{"sdk/nodejs/types/input.ts",
			"groups?: pulumi.Input<pulumi.Input<inputs.InboxForwarderMatchGroupArgs>[]"},
		{"sdk/python/pulumi_mailslurp/_inputs.py",
			"groups: pulumi.Input[Optional[Sequence[pulumi.Input['InboxForwarderMatchGroupArgs']]]]"},
		{"sdk/dotnet/Inputs/InboxForwarderMatchGroupArgs.cs",
			"InputList<Inputs.InboxForwarderMatchGroupArgs> Groups"},
		{"sdk/go/mailslurp/pulumiTypes.go",
			"Groups []InboxForwarderMatchGroup `pulumi:\"groups\"`"},
	}
}

// TestTheRecursiveTypeReachesEveryLanguage reads the self-reference out of each SDK. The
// schema declares `groups` as an array of the type that holds it.
func TestTheRecursiveTypeReachesEveryLanguage(t *testing.T) {
	t.Parallel()
	for _, found := range recursiveType() {
		t.Run(filepath.Base(found.path), func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, string(readRepoBytes(t, found.path)), found.property,
				"%s declares a match group that does not hold match groups", found.path)
		})
	}
}

// Every member of the Python enums, and the two the generator has to rename. `and` and `or` are
// Python keywords, so the generator appends an underscore rather than emit a syntax error.
func TestThePythonEnumMembersKeepTheirWireValues(t *testing.T) {
	t.Parallel()
	member := regexp.MustCompile(`(?m)^    ([A-Za-z_][A-Za-z_0-9]*) = "([^"]+)"$`)
	found := member.FindAllStringSubmatch(string(readRepoBytes(t, filepath.Join(pythonPackage, "_enums.py"))), -1)

	total := 0
	for _, enum := range schemaEnums(t) {
		total += len(enum.values)
	}
	require.Len(t, found, total, "the Python SDK publishes a different number of enum members")

	keywordSafe := map[string]bool{"AND_": false, "OR_": false}
	for _, entry := range found {
		if _, exception := keywordSafe[entry[1]]; exception {
			keywordSafe[entry[1]] = true
			assert.Equal(t, entry[1], entry[2]+"_", "%s is not the keyword form of %s", entry[1], entry[2])
			continue
		}
		assert.Equal(t, entry[2], entry[1],
			"the Python SDK spells the member of %q differently from the value it sends", entry[2])
	}
	for name, seen := range keywordSafe {
		assert.True(t, seen, "the Python SDK no longer spells a keyword member %s; the exception above is stale", name)
	}
}

// TestTheDotnetPackageIconIsTheMarkThisRepoAuthored compares the packaged icon with the source
// image. The generator writes whatever the logo URL returns, so an error page reaches the package.
func TestTheDotnetPackageIconIsTheMarkThisRepoAuthored(t *testing.T) {
	t.Parallel()
	source := readRepoBytes(t, logoRasterPath)
	packaged := readRepoBytes(t, dotnetIconPath)
	assert.True(t, bytes.HasPrefix(packaged, pngMagic),
		"%s is not a PNG: it starts %q", dotnetIconPath, packaged[:min(len(packaged), 16)])
	assert.True(t, bytes.Equal(source, packaged),
		"%s holds %d bytes and %s holds %d, so the package ships a different mark",
		logoRasterPath, len(source), dotnetIconPath, len(packaged))
	assert.Contains(t, string(readRepoBytes(t, dotnetProject)), "<PackageIcon>logo.png</PackageIcon>",
		"%s names no package icon, so nothing reads %s", dotnetProject, dotnetIconPath)
}

// recipeFor answers the commands one make target runs, without running them.
func recipeFor(t *testing.T, target string) string {
	t.Helper()
	out, err := runMake(t, "-n", target)
	require.NoError(t, err, "make -n %s: %s", target, out)
	return out
}

// The generator replaces its language directory whole, so the icon has to be written back after
// every generation or the committed file is the downloaded body again.
func TestTheDotnetGenerationRestoresThePackageIcon(t *testing.T) {
	t.Parallel()
	recipe := recipeFor(t, "generate_dotnet")
	generated := strings.Index(recipe, "gen-sdk")
	restored := strings.Index(recipe, logoRasterPath)
	require.GreaterOrEqual(t, generated, 0, "make generate_dotnet runs no generator: %s", recipe)
	require.GreaterOrEqual(t, restored, 0,
		"make generate_dotnet never writes %s back: %s", dotnetIconPath, recipe)
	assert.Greater(t, restored, generated,
		"make generate_dotnet restores the icon before the generator overwrites it: %s", recipe)
}

// The CLI logs in when a token is in the environment and writes credentials into the plugin
// home. Generation needs no account, so the recipes clear the token rather than carry it.
func TestTheGenerationRecipesCarryNoAccessToken(t *testing.T) {
	t.Parallel()
	for _, language := range sdkLanguages() {
		recipe := recipeFor(t, "generate_"+language)
		assert.Contains(t, recipe, "PULUMI_ACCESS_TOKEN= ",
			"make generate_%s runs the generator with whatever token is in the environment: %s",
			language, recipe)
	}
	assert.NoFileExists(t, filepath.Join(repoRoot(t), ".pulumi", "credentials.json"),
		"a generation left credentials in the plugin home")
}

// The generator writes no go.mod, and the SDK is a module of its own so the provider's tests do
// not compile it. The recipe rebuilds the file with versions read from the provider.
func TestTheGoGenerationRebuildsTheModuleFile(t *testing.T) {
	t.Parallel()
	recipe := recipeFor(t, "generate_go")
	assert.Contains(t, recipe, "go mod init "+goImportBasePath,
		"make generate_go leaves the Go SDK without its module path: %s", recipe)

	module := string(readRepoBytes(t, goModulePath))
	root := string(readRepoBytes(t, "go.mod"))
	for _, line := range []string{goDirective, "github.com/pulumi/pulumi/sdk/v3 v3.258.0"} {
		assert.Contains(t, module, line, "%s and the provider disagree about %q", goModulePath, line)
		assert.Contains(t, root, line, "go.mod no longer pins %q", line)
	}
}

// The check below rewrites four SDK directories, which destroys the staged builds that the live
// runs need. Its own target runs it, and the target that calls the API leaves it alone.
func TestOnlyItsOwnTargetRunsTheGenerationCheck(t *testing.T) {
	t.Parallel()
	const check = "TestGeneratingTheSDKsLeavesTheTreeClean"
	assert.Contains(t, recipeFor(t, "test_generated"), "-run "+check,
		"make test_generated no longer runs the generation check")
	assert.Contains(t, recipeFor(t, "test_integration"), "-skip "+check,
		"make test_integration regenerates the SDKs, and the staged builds do not survive it")
}

// TestGeneratingTheSDKsLeavesTheTreeClean regenerates all four trees over the committed ones.
// A file that nothing writes back is gone afterwards, and a file that differs shows up here.
func TestGeneratingTheSDKsLeavesTheTreeClean(t *testing.T) {
	if testing.Short() {
		t.Skip("the generation needs the Pulumi CLI and rewrites four directories")
	}
	for _, language := range sdkLanguages() {
		out, err := runMake(t, "generate_"+language)
		require.NoError(t, err, "make generate_%s: %s", language, out)
	}

	command := exec.Command("git", "status", "--porcelain", "--", "sdk")
	command.Dir = repoRoot(t)
	changed, err := command.Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(changed)),
		"a fresh generation does not reproduce the committed SDKs:\n%s", changed)
}
