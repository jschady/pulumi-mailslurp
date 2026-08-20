package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const registryPRPage = "docs/registry-pr.md"

// The page claims things about this repository, and a reader works through it at the publish gate
// with no way to check them. Each claim below is read back from what it describes.

// pageClaimsCount answers the number the page states before one phrase, such as "5 resources".
func pageClaimsCount(t *testing.T, page, noun string) int {
	t.Helper()
	found := regexp.MustCompile(`([0-9]+) ` + regexp.QuoteMeta(noun)).FindStringSubmatch(page)
	require.NotNil(t, found, "%s states no count of %s", registryPRPage, noun)
	counted, err := strconv.Atoi(found[1])
	require.NoError(t, err)
	return counted
}

func TestTheRegistryPageCountsWhatTheSchemaPublishes(t *testing.T) {
	page := readRepoFile(t, registryPRPage)

	var schema struct {
		Resources map[string]json.RawMessage `json:"resources"`
		Functions map[string]json.RawMessage `json:"functions"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &schema))

	assert.Equal(t, len(schema.Resources), pageClaimsCount(t, page, "resources"))
	assert.Equal(t, len(schema.Functions), pageClaimsCount(t, page, "functions"))
	assert.Equal(t, len(sdkLanguages()), pageClaimsCount(t, page, "languages"))
}

// The pull request entry names the schema by path, and a wrong path is a blocking check on a
// pull request nobody can fix from the registry side.
func TestTheRegistryPageNamesTheCommittedSchema(t *testing.T) {
	page := readRepoFile(t, registryPRPage)
	assert.Contains(t, page, committedSchemaFile)
	assert.FileExists(t, filepath.Join(repoRoot(t), committedSchemaFile))
}

// Every file the checklist sends a reader to has to be there when they open it.
func TestTheRegistryPageNamesFilesThatExist(t *testing.T) {
	page := readRepoFile(t, registryPRPage)

	named := regexp.MustCompile("`(?:\\./)?((?:docs|scripts|provider)/[A-Za-z0-9._/-]+|\\.goreleaser\\.yml)`").
		FindAllStringSubmatch(page, -1)
	require.NotEmpty(t, named, "%s names no file in this repository", registryPRPage)

	seen := map[string]bool{}
	for _, match := range named {
		if seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		assert.FileExists(t, filepath.Join(repoRoot(t), match[1]),
			"%s sends a reader to %s", registryPRPage, match[1])
	}
	assert.GreaterOrEqual(t, len(seen), 5, "the scan read %d files and the page names more", len(seen))
}

// theInstallCommand answers the command a reader copies out of the page. The table elsewhere names
// the same command in prose, so a scan of the whole page passes on a runnable line that is wrong.
func theInstallCommand(t *testing.T, page string) string {
	t.Helper()
	var found []string
	for fence, block := range strings.Split(page, "```") {
		if fence%2 == 0 {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			if strings.Contains(line, "pulumi plugin install") {
				found = append(found, strings.TrimSpace(line))
			}
		}
	}
	require.Len(t, found, 1, "%s should carry one runnable install command, and it carries %v",
		registryPRPage, found)
	return found[0]
}

// The install command is the one real test of the download server, so it names the server the
// schema advertises rather than one somebody typed.
func TestTheRegistryPageInstallsFromTheServerTheSchemaAdvertises(t *testing.T) {
	var advertised struct {
		PluginDownloadURL string `json:"pluginDownloadURL"`
		Name              string `json:"name"`
		Publisher         string `json:"publisher"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &advertised))

	page := readRepoFile(t, registryPRPage)
	command := theInstallCommand(t, page)
	assert.Contains(t, command, "pulumi plugin install resource "+advertised.Name+" ")
	assert.Contains(t, command, "--server "+advertised.PluginDownloadURL)
	assert.Contains(t, page, `"`+advertised.Publisher+`": "`+advertised.Publisher+`"`,
		"the publisher entry must carry the publisher the schema sets")
}

// The mark is a user-approval item, so the page describes the artwork the approver opens.
func TestTheRegistryPageDescribesTheCommittedMark(t *testing.T) {
	page := readRepoFile(t, registryPRPage)
	logo := readRepoFile(t, logoFile)

	// The list an approver reads, item by item. The prose elsewhere on the page names the same
	// parts, so a check that scanned the whole page would pass on a rewritten list.
	drawn := regexp.MustCompile(`(?m)^- (?:a|one) (.+)$`).FindAllStringSubmatch(page, -1)
	described := map[string]bool{}
	for _, item := range drawn {
		described[item[1]] = true
	}
	for colour, part := range map[string]string{
		"#232a3d": "navy tile", "#ffffff": "white geometric letter M", "#f2994a": "amber circle",
	} {
		assert.Containsf(t, logo, colour, "the mark should draw the %s the page describes", part)
		assert.Truef(t, described[part], "the list an approver reads should name the %s, and it lists %v",
			part, drawn)
	}

	raster, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "logo.png"))
	require.NoError(t, err, "NuGet rejects an SVG icon, so the raster copy ships as well")
	require.Greater(t, len(raster), 24)
	assert.Equal(t, []byte{0, 0, 1, 0, 0, 0, 1, 0}, raster[16:24],
		"the page tells a reader to write a 256 by 256 pixel copy")
}

// The two Python members the generator renames are a stated limitation, and the page states the
// count of the members it leaves alone.
func TestTheRegistryPageCountsThePythonEnumMembers(t *testing.T) {
	page := readRepoFile(t, registryPRPage)
	member := regexp.MustCompile(`(?m)^\s+([A-Z][A-Z0-9_]*) = "([^"]+)"`)

	var verbatim, renamed int
	for _, found := range member.FindAllStringSubmatch(
		string(readRepoBytes(t, filepath.Join(pythonPackage, "_enums.py"))), -1) {
		if found[1] == found[2] {
			verbatim++
			continue
		}
		renamed++
		assert.Contains(t, page, "`"+found[1]+"`", "the page must name the renamed member %s", found[1])
	}
	require.NotZero(t, verbatim)
	assert.Equal(t, verbatim, pageClaimsCount(t, page, "members spell the wire value exactly"))

	whole := regexp.MustCompile(`renames any of the ([0-9]+)`).FindStringSubmatch(page)
	require.NotNil(t, whole, "%s states no count of the members the test guards", registryPRPage)
	assert.Equal(t, strconv.Itoa(verbatim+renamed), whole[1])
}

// The prerelease warning reads the release config. A config that stopped marking prereleases would
// leave the page telling a reader to expect a check failure that no longer happens.
func TestTheRegistryPageWarnsAboutTheSettingTheReleaseConfigCarries(t *testing.T) {
	page := readRepoFile(t, registryPRPage)
	require.Equal(t, "auto", readReleaseConfig(t).Release.Prerelease)
	assert.Contains(t, page, "`prerelease: auto`",
		"the page must name the setting that turns a suffixed tag into a prerelease")
	assert.Contains(t, strings.ToLower(page), "published release")
}
