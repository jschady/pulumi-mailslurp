package provider

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	releaseConfigFile   = ".goreleaser.yml"
	archiveNameTemplate = "{{ .Binary }}-{{ .Tag }}-{{ .Os }}-{{ .Arch }}"
	providerBinaryName  = "pulumi-resource-mailslurp"
	snapshotWorkflow    = "main.yml"
	releaseWorkflow     = "release.yml"
)

// releaseConfigSections is every top-level key the release config may carry. A key outside this
// list uploads somewhere, signs something, or builds an image, and none of that belongs here.
var releaseConfigSections = []string{
	"archives", "builds", "changelog", "project_name", "release", "version",
}

// archiveCountPattern reads the count the snapshot job compares against. Searching the job for the
// number alone matches the arm64 in the same step, which pins nothing.
var archiveCountPattern = regexp.MustCompile(`-ne ([0-9]+)`)

type releaseBuild struct {
	ID      string   `yaml:"id"`
	Dir     string   `yaml:"dir"`
	Main    string   `yaml:"main"`
	Binary  string   `yaml:"binary"`
	Env     []string `yaml:"env"`
	Goos    []string `yaml:"goos"`
	Goarch  []string `yaml:"goarch"`
	Ldflags []string `yaml:"ldflags"`
}

type releaseArchive struct {
	ID           string   `yaml:"id"`
	IDs          []string `yaml:"ids"`
	Formats      []string `yaml:"formats"`
	NameTemplate string   `yaml:"name_template"`
}

type releaseConfig struct {
	Version     int              `yaml:"version"`
	ProjectName string           `yaml:"project_name"`
	Builds      []releaseBuild   `yaml:"builds"`
	Archives    []releaseArchive `yaml:"archives"`
	Changelog   struct {
		Disable bool `yaml:"disable"`
	} `yaml:"changelog"`
	Release struct {
		Prerelease string `yaml:"prerelease"`
	} `yaml:"release"`
}

func readReleaseConfig(t *testing.T) releaseConfig {
	t.Helper()
	var config releaseConfig
	require.NoError(t, yaml.Unmarshal([]byte(readRepoFile(t, releaseConfigFile)), &config),
		"%s must parse as YAML", releaseConfigFile)
	return config
}

// releaseConfigKeys answers the top-level keys of the release config, read from the document
// itself rather than from the struct above, which would silently drop an unknown one.
func releaseConfigKeys(t *testing.T) []string {
	t.Helper()
	var document map[string]yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(readRepoFile(t, releaseConfigFile)), &document))

	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// The one build a release runs. Everything below reads it, so a second build would leave half of
// these assertions reading a config nobody else does.
func theReleaseBuild(t *testing.T) releaseBuild {
	t.Helper()
	config := readReleaseConfig(t)
	require.Len(t, config.Builds, 1, "%s must declare one build", releaseConfigFile)
	return config.Builds[0]
}

func TestTheReleaseConfigDeclaresOnlyTheSectionsAReleaseNeeds(t *testing.T) {
	assert.ElementsMatch(t, releaseConfigSections, releaseConfigKeys(t),
		"%s must carry these sections and no other", releaseConfigFile)

	body := readRepoFile(t, releaseConfigFile)
	for _, forbidden := range []string{"blobs", "signs", "binary_signs", "notarize", "hooks", "dockers"} {
		assert.NotContains(t, body, forbidden+":",
			"%s declares %s, which uploads or signs outside this repository", releaseConfigFile, forbidden)
	}
}

func TestTheReleaseConfigStampsOneVersionSymbol(t *testing.T) {
	config := readReleaseConfig(t)
	assert.Equal(t, 2, config.Version, "%s must declare the schema version the tool reads", releaseConfigFile)
	assert.Equal(t, "pulumi-mailslurp", config.ProjectName)

	var stamps []string
	for _, flag := range theReleaseBuild(t).Ldflags {
		if strings.HasPrefix(flag, "-X") {
			stamps = append(stamps, flag)
		}
	}
	require.Len(t, stamps, 1, "the release build must stamp one version symbol, and it stamps %v", stamps)

	symbol, value, found := strings.Cut(strings.TrimSpace(strings.TrimPrefix(stamps[0], "-X")), "=")
	require.True(t, found, "the linker flag %s assigns nothing", stamps[0])
	assert.Equal(t, strings.TrimSuffix(strings.TrimPrefix(versionLdflag, "-X "), "="), symbol,
		"the release must stamp the symbol the provider reads")
	assert.Equal(t, "{{.Tag}}", value, "the release must stamp the tag it builds")
}

func TestTheReleaseConfigCoversEveryPlatform(t *testing.T) {
	build := theReleaseBuild(t)
	assert.ElementsMatch(t, []string{"darwin", "linux", "windows"}, build.Goos)
	assert.ElementsMatch(t, []string{"amd64", "arm64"}, build.Goarch)
	assert.ElementsMatch(t, []string{"CGO_ENABLED=0"}, build.Env,
		"a release binary that links libc runs on fewer machines than the plugin loader reaches")
	assert.Equal(t, providerBinaryName, build.Binary,
		"the release must build the binary the plugin loader runs")
	assert.Equal(t, "provider", build.Dir)
	assert.Equal(t, "./cmd/"+providerBinaryName+"/", build.Main)
}

func TestTheReleaseArchivesCarryTheNameThePluginLoaderAsks(t *testing.T) {
	config := readReleaseConfig(t)
	require.Len(t, config.Archives, 1, "%s must declare one archive", releaseConfigFile)
	archive := config.Archives[0]

	assert.Equal(t, archiveNameTemplate, archive.NameTemplate,
		"the plugin download reads the archive name, so the template is the install")
	assert.ElementsMatch(t, []string{"tar.gz"}, archive.Formats)
	assert.ElementsMatch(t, []string{theReleaseBuild(t).ID}, archive.IDs,
		"the archive must hold the build this config declares")

	assert.Equal(t, []string{
		"pulumi-resource-mailslurp-v9.9.9-darwin-amd64.tar.gz",
		"pulumi-resource-mailslurp-v9.9.9-darwin-arm64.tar.gz",
		"pulumi-resource-mailslurp-v9.9.9-linux-amd64.tar.gz",
		"pulumi-resource-mailslurp-v9.9.9-linux-arm64.tar.gz",
		"pulumi-resource-mailslurp-v9.9.9-windows-amd64.tar.gz",
		"pulumi-resource-mailslurp-v9.9.9-windows-arm64.tar.gz",
	}, expectedArchiveNames(t, "v9.9.9"))
}

func TestTheReleaseWritesNoChangelogAndMarksAPrereleaseTag(t *testing.T) {
	config := readReleaseConfig(t)
	assert.True(t, config.Changelog.Disable, "this repository writes no changelog")
	assert.Equal(t, "auto", config.Release.Prerelease,
		"a tag carrying a prerelease suffix must reach GitHub as a prerelease")
}

// expectedArchiveNames renders the archive template over every platform pair the config lists.
func expectedArchiveNames(t *testing.T, tag string) []string {
	t.Helper()
	config := readReleaseConfig(t)
	build := theReleaseBuild(t)
	fixed := strings.NewReplacer("{{ .Binary }}", build.Binary, "{{ .Tag }}", tag).
		Replace(config.Archives[0].NameTemplate)

	var names []string
	for _, goos := range build.Goos {
		for _, goarch := range build.Goarch {
			names = append(names,
				strings.NewReplacer("{{ .Os }}", goos, "{{ .Arch }}", goarch).Replace(fixed)+".tar.gz")
		}
	}
	sort.Strings(names)
	return names
}

// workflowJobBody answers the body of one job. The scan stops at the next job header: a step in a
// different job runs on a different runner, and it satisfies nothing this job needs.
func workflowJobBody(t *testing.T, workflow, job string) string {
	t.Helper()
	body := readRepoFile(t, filepath.Join(".github", "workflows", workflow))
	lines := strings.Split(body[strings.Index(body, "\njobs:\n"):], "\n")

	var collected []string
	inside := false
	for _, line := range lines {
		header := strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") &&
			strings.HasSuffix(strings.TrimRight(line, " "), ":")
		if header {
			if inside {
				break
			}
			inside = strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(line, " "), ":")) == job
			continue
		}
		if inside {
			collected = append(collected, line)
		}
	}
	require.True(t, inside, "%s declares no %s job", workflow, job)
	return strings.Join(collected, "\n")
}

// The snapshot job is the only run that exercises the release before a tag exists. It built the
// binaries and stopped once, so a wrong archive template would have reached a real release.
func TestTheSnapshotJobCountsEveryArchive(t *testing.T) {
	job := workflowJobBody(t, snapshotWorkflow, "snapshot")

	assert.Contains(t, job, "release --snapshot",
		"the snapshot job must archive the binaries, and a plain build writes no archive")
	assert.Contains(t, job, "artifacts.json",
		"the snapshot job must read the list of what the release produced")

	counted := archiveCountPattern.FindStringSubmatch(job)
	require.NotNil(t, counted, "the snapshot job compares the archive count with nothing")
	assert.Equal(t, strconv.Itoa(len(expectedArchiveNames(t, "v0.0.0"))), counted[1],
		"the snapshot job accepts a different number of archives than the config builds")
}

// 60m0s is goreleaser's own default today, and the workflows install a floating `~> v2` release.
// Naming the deadline keeps a later default change away from this eight-binary cross-compile.
func TestBothGoreleaserRunsNameTheTimeout(t *testing.T) {
	for _, tc := range []struct{ workflow, job string }{
		{snapshotWorkflow, "snapshot"},
		{releaseWorkflow, "publish_provider"},
	} {
		t.Run(tc.workflow, func(t *testing.T) {
			assert.Contains(t, workflowJobBody(t, tc.workflow, tc.job), "--timeout 60m0s",
				"the goreleaser run of %s does not name its deadline", tc.job)
		})
	}
}

// goreleaser reads the tag history to name the release, and a second tag on the same commit makes
// that reading ambiguous. The publish job names the tag it builds.
func TestThePublishJobNamesTheTagItBuilds(t *testing.T) {
	job := workflowJobBody(t, releaseWorkflow, "publish_provider")
	assert.Contains(t, job, "GORELEASER_CURRENT_TAG: v${{ steps.version.outputs.version }}",
		"the publish job leaves goreleaser to guess the tag it builds")
	assert.Contains(t, job, "id: version",
		"the version step declares no identifier, so the tag expression reads nothing")
}

// The release config sits at the repository root, where goreleaser reads it without an argument.
func TestTheReleaseConfigIsTheOnlyOne(t *testing.T) {
	found, err := filepath.Glob(filepath.Join(repoRoot(t), ".goreleaser*"))
	require.NoError(t, err)

	var names []string
	for _, path := range found {
		names = append(names, filepath.Base(path))
	}
	assert.Equal(t, []string{releaseConfigFile}, names,
		"a second release config would publish a release nobody read")
}

// TestTheSnapshotTargetRunsTheReleaseCommand reads the recipe. A target that builds without
// archiving leaves the archive names unexercised until a tag publishes them.
func TestTheSnapshotTargetRunsTheReleaseCommand(t *testing.T) {
	out, err := runMake(t, "-n", "release_snapshot")
	require.NoError(t, err, out)
	assert.Contains(t, out, "goreleaser release --snapshot --clean")
}
