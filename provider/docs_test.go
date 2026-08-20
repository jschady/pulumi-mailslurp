package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

const (
	exampleSourceDir    = "docs/yaml"
	exampleEmbedDir     = "provider/internal/embed"
	committedSchemaFile = "provider/cmd/pulumi-resource-mailslurp/schema.json"

	exampleUsageHeading = "## Example Usage"
	examplesOpen        = "{{% examples %}}"
	examplesClose       = "{{% /examples %}}"
	exampleOpen         = "{{% example %}}"
	exampleClose        = "{{% /example %}}"
	fenceMarker         = "```"
)

// exampleLanguages are the fenced blocks of one example, in the order the generator writes them.
func exampleLanguages() []string { return []string{"typescript", pythonLanguage, "csharp", "go"} }

// exampleStem answers the file name that the source and the embed file of one resource share.
func exampleStem(token string) string {
	parts := strings.Split(token, ":")
	return strings.ToLower(parts[len(parts)-1])
}

// schemaResources is the part of a schema document that the example checks read.
type schemaResources struct {
	Resources map[string]struct {
		Description string `json:"description"`
	} `json:"resources"`
}

func resourceDescriptions(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var doc schemaResources
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Resources, "the schema publishes no resource")

	out := map[string]string{}
	for token, resource := range doc.Resources {
		out[token] = resource.Description
	}
	return out
}

func committedSchema(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), committedSchemaFile))
	require.NoError(t, err)
	return raw
}

// builtSchema answers the schema of the running provider, which carries what Annotate wrote.
func builtSchema(t *testing.T) []byte {
	t.Helper()
	server, err := New(nil)
	require.NoError(t, err)
	resp, err := server.GetSchema(context.Background(), &rpc.GetSchemaRequest{Version: 0})
	require.NoError(t, err)
	return []byte(resp.GetSchema())
}

func exampleStems(t *testing.T) []string {
	t.Helper()
	var stems []string
	for token := range resourceDescriptions(t, committedSchema(t)) {
		stems = append(stems, exampleStem(token))
	}
	sort.Strings(stems)
	return stems
}

func exampleSourcePath(t *testing.T, stem string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), exampleSourceDir, stem+"-examples.yaml")
}

func embedPath(t *testing.T, stem string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), exampleEmbedDir, stem+"-examples.md")
}

// readExampleSource answers every document of one multi-document example source.
func readExampleSource(t *testing.T, stem string) []map[string]any {
	t.Helper()
	file, err := os.Open(exampleSourcePath(t, stem))
	require.NoError(t, err, "every published resource ships an example source")
	defer func() { require.NoError(t, file.Close()) }()

	decoder := yaml.NewDecoder(file)
	var docs []map[string]any
	for {
		doc := map[string]any{}
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, "%s must parse as YAML", stem)
		docs = append(docs, doc)
	}
	require.NotEmpty(t, docs, "%s must carry at least one example", stem)
	return docs
}

func readEmbedFile(t *testing.T, stem string) string {
	t.Helper()
	raw, err := os.ReadFile(embedPath(t, stem))
	require.NoError(t, err, "make docs writes one embed file per resource")
	return string(raw)
}

// exampleBlocks answers the text between each example shortcode pair.
func exampleBlocks(t *testing.T, markdown string) []string {
	t.Helper()
	var blocks []string
	rest := markdown
	for {
		open := strings.Index(rest, exampleOpen)
		if open < 0 {
			return blocks
		}
		rest = rest[open+len(exampleOpen):]
		end := strings.Index(rest, exampleClose)
		require.GreaterOrEqual(t, end, 0, "an example opens and never closes")
		blocks = append(blocks, rest[:end])
		rest = rest[end+len(exampleClose):]
	}
}

// fenceBody answers the text inside the fenced block of one language.
func fenceBody(t *testing.T, block, tag string) string {
	t.Helper()
	open := fenceMarker + tag + "\n"
	start := strings.Index(block, open)
	require.GreaterOrEqual(t, start, 0, "the example carries no %s block", tag)
	rest := block[start+len(open):]
	end := strings.Index(rest, "\n"+fenceMarker)
	require.GreaterOrEqual(t, end, 0, "the %s block never closes", tag)
	return rest[:end]
}

// fenceTags answers the language tag of every opening fence, and the total number of fence lines.
func fenceTags(block string) ([]string, int) {
	var tags []string
	lines := 0
	open := false
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, fenceMarker) {
			continue
		}
		lines++
		if open {
			open = false
			continue
		}
		open = true
		tags = append(tags, strings.TrimPrefix(line, fenceMarker))
	}
	return tags, lines
}

func TestEveryPublishedResourceShipsAnExampleSource(t *testing.T) {
	t.Parallel()
	stems := exampleStems(t)
	require.Len(t, stems, 5, "the provider publishes five resources")

	for _, stem := range stems {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			docs := readExampleSource(t, stem)
			for i, doc := range docs {
				description, ok := doc["description"].(string)
				assert.True(t, ok, "example %d of %s must carry a description", i, stem)
				assert.NotEmpty(t, description)
				assert.Equal(t, "yaml", doc["runtime"], "example %d of %s must be a YAML program", i, stem)
			}
		})
	}
}

// The registry reads the shortcodes to split the examples, so a missing wrapper hides them all.
func TestEveryEmbedFileWrapsItsExamplesInTheRegistryShortcodes(t *testing.T) {
	t.Parallel()
	for _, stem := range exampleStems(t) {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			markdown := readEmbedFile(t, stem)
			assert.True(t, strings.HasPrefix(markdown, examplesOpen+"\n"+exampleUsageHeading+"\n"),
				"%s must open with the examples wrapper and the heading", stem)
			// The prose check reads every schema description as one stream, and the closing
			// newline is what keeps this page apart from the description that follows it.
			assert.True(t, strings.HasSuffix(markdown, examplesClose+"\n"),
				"%s must close the wrapper on a line of its own", stem)
			assert.Equal(t, len(readExampleSource(t, stem)), strings.Count(markdown, exampleOpen),
				"%s must carry one example block per source document", stem)
			assert.Equal(t, strings.Count(markdown, exampleOpen), strings.Count(markdown, exampleClose),
				"%s must close every example block", stem)
		})
	}
}

func TestEveryExampleCarriesOneFencedBlockPerLanguage(t *testing.T) {
	t.Parallel()
	for _, stem := range exampleStems(t) {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			blocks := exampleBlocks(t, readEmbedFile(t, stem))
			require.NotEmpty(t, blocks)
			for i, block := range blocks {
				tags, lines := fenceTags(block)
				assert.Equal(t, exampleLanguages(), tags,
					"example %d of %s must carry one block per language, in order", i, stem)
				assert.Equal(t, 2*len(exampleLanguages()), lines,
					"example %d of %s must open and close every fenced block", i, stem)
			}
		})
	}
}

// The heading is what the registry prints above the code, so it must repeat the source description.
func TestEveryExampleHeadingRepeatsItsSourceDescription(t *testing.T) {
	t.Parallel()
	for _, stem := range exampleStems(t) {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			docs := readExampleSource(t, stem)
			blocks := exampleBlocks(t, readEmbedFile(t, stem))
			require.Len(t, blocks, len(docs))
			for i, block := range blocks {
				assert.True(t, strings.HasPrefix(block, "\n### "+docs[i]["description"].(string)+"\n"),
					"example %d of %s must open with the heading of its source", i, stem)
			}
		})
	}
}

// resourceConstructors answers how each language spells the construction of one resource. The
// marker is the namespace that the schema name fixes, so a package rename leaves the markers alone.
func resourceConstructors() map[string]*regexp.Regexp {
	return map[string]*regexp.Regexp{
		"typescript": regexp.MustCompile(`new mailslurp\.`),
		"python":     regexp.MustCompile(`(?m)^\w+ = mailslurp\.`),
		"csharp":     regexp.MustCompile(`new Mailslurp\.`),
		"go":         regexp.MustCompile(`mailslurp\.New`),
	}
}

// The Pulumi CLI reports an unknown resource or property and still writes a program, so a page can
// look whole while it teaches nothing. Every resource of the source must reach every program.
func TestEveryExampleProgramCreatesEveryResourceOfItsSource(t *testing.T) {
	t.Parallel()
	constructors := resourceConstructors()
	for _, stem := range exampleStems(t) {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			docs := readExampleSource(t, stem)
			blocks := exampleBlocks(t, readEmbedFile(t, stem))
			require.Len(t, blocks, len(docs))
			for i, block := range blocks {
				resources, ok := docs[i]["resources"].(map[string]any)
				require.True(t, ok, "example %d of %s declares no resource", i, stem)
				require.NotEmpty(t, resources)
				for _, language := range exampleLanguages() {
					constructor, ok := constructors[language]
					require.True(t, ok, "the %s block has no constructor marker", language)
					assert.Len(t, constructor.FindAllString(fenceBody(t, block, language), -1),
						len(resources),
						"example %d of %s drops a resource on the way to the %s program", i, stem, language)
				}
			}
		})
	}
}

// Annotate appends the embed file to each description, and the registry page renders that text.
func TestEveryResourceDescriptionCarriesItsExamples(t *testing.T) {
	t.Parallel()
	descriptions := resourceDescriptions(t, builtSchema(t))
	require.Len(t, descriptions, 5)

	for token, description := range descriptions {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, description, exampleUsageHeading,
				"%s must document its examples", token)
			assert.Contains(t, description, readEmbedFile(t, exampleStem(token)),
				"%s must append its whole embed file", token)
		})
	}
}

// The shipped file feeds the registry, so the examples must reach it and not the built server alone.
func TestTheCommittedSchemaCarriesTheExamplesOfEveryResource(t *testing.T) {
	t.Parallel()
	for token, description := range resourceDescriptions(t, committedSchema(t)) {
		assert.Contains(t, description, exampleUsageHeading, "%s", token)
		assert.Contains(t, description, examplesOpen, "%s", token)
	}
}

// An example that names an API key teaches a reader to commit one. The key comes from the
// environment or from the stack configuration, never from the program text.
func TestNoExampleNamesAnAPIKey(t *testing.T) {
	t.Parallel()
	for _, stem := range exampleStems(t) {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			for name, path := range map[string]string{
				"source": exampleSourcePath(t, stem),
				"embed":  embedPath(t, stem),
			} {
				raw, err := os.ReadFile(path)
				require.NoError(t, err)
				text := strings.ToLower(string(raw))
				assert.NotContains(t, text, "apikey", "the %s of %s names an API key", name, stem)
				assert.NotContains(t, text, "mailslurp_api_key",
					"the %s of %s names the API key variable", name, stem)
			}
		})
	}
}

// exampleDescription is one description of one example source, and the place a reader can edit it.
type exampleDescription struct {
	text   string
	source string
	line   int
}

// collectDescriptions answers every description of one YAML document, at any depth, with the line
// each one sits on. The parsed map loses the line, so this reads the nodes.
func collectDescriptions(node *yaml.Node, source string, out *[]exampleDescription) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			collectDescriptions(child, source, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value == "description" && value.Kind == yaml.ScalarNode {
				*out = append(*out, exampleDescription{text: value.Value, source: source, line: value.Line})
				continue
			}
			collectDescriptions(value, source, out)
		}
	}
}

// readExampleNodes answers the YAML nodes of every document of one example source.
func readExampleNodes(t *testing.T, stem string) []*yaml.Node {
	t.Helper()
	file, err := os.Open(exampleSourcePath(t, stem))
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	decoder := yaml.NewDecoder(file)
	var docs []*yaml.Node
	for {
		node := &yaml.Node{}
		err := decoder.Decode(node)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		docs = append(docs, node)
	}
	require.NotEmpty(t, docs)
	return docs
}

// proseSeed writes every description into one page, and answers the map from a line of that page
// to the source that wrote it.
func proseSeed(t *testing.T, descriptions []exampleDescription) (string, map[int]string) {
	t.Helper()
	sources := map[int]string{}
	body := "# The example descriptions\n\n"
	line := 3

	for _, description := range descriptions {
		sources[line] = fmt.Sprintf("%s:%d", description.source, description.line)
		body += description.text + "\n\n"
		line += strings.Count(description.text, "\n") + 2
	}

	seed := filepath.Join(t.TempDir(), "example-descriptions.md")
	writeFile(t, seed, body)
	return seed, sources
}

// nameTheSource rewrites every finding of the seeded page so it names the file a reader can open.
// The page itself lives for one run, so its own line numbers help nobody.
func nameTheSource(report, seed string, sources map[int]string) string {
	var named []string
	for _, finding := range strings.Split(report, "\n") {
		rest, found := strings.CutPrefix(finding, seed+":")
		if !found {
			named = append(named, finding)
			continue
		}
		number, tail, _ := strings.Cut(rest, ":")
		reported, err := strconv.Atoi(number)
		if err != nil {
			named = append(named, finding)
			continue
		}
		start := 0
		for line := range sources {
			if line <= reported && line > start {
				start = line
			}
		}
		if start == 0 {
			named = append(named, finding)
			continue
		}
		named = append(named, sources[start]+":"+tail)
	}
	return strings.Join(named, "\n")
}

func TestEveryExampleDescriptionPassesTheProseCheck(t *testing.T) {
	var descriptions []exampleDescription
	for _, stem := range exampleStems(t) {
		source := filepath.Join(exampleSourceDir, stem+"-examples.yaml")
		for _, node := range readExampleNodes(t, stem) {
			collectDescriptions(node, source, &descriptions)
		}
	}
	require.NotEmpty(t, descriptions)

	seed, sources := proseSeed(t, descriptions)
	out, err := runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.NoError(t, err, "the example descriptions must pass the prose check:\n%s",
		nameTheSource(out, seed, sources))
}

// A finding that names the seeded page names a file that lives for one run. This drives two
// descriptions through the real check and reads the report a maintainer would act on.
func TestAProseFindingNamesTheExampleSourceAndItsLine(t *testing.T) {
	descriptions := []exampleDescription{
		{text: "An inbox that receives email", source: "docs/yaml/first-examples.yaml", line: 2},
		{text: "A webhook that you can utilize", source: "docs/yaml/second-examples.yaml", line: 9},
	}

	seed, sources := proseSeed(t, descriptions)
	out, err := runMake(t, "lint_prose", "PROSE_FILES="+seed)
	require.Error(t, err, "the check accepted a banned word: %s", out)

	named := nameTheSource(out, seed, sources)
	assert.Contains(t, named, "docs/yaml/second-examples.yaml:9:", "the report must name the source")
	// make echoes the command it runs, so the page can appear as an argument. No FINDING may
	// carry it: a finding is the line that a maintainer acts on.
	assert.NotContains(t, named, seed+":", "no finding may name the seeded page")
}

// snapshotPages answers the bytes of every generated example file in one directory.
func snapshotPages(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	out := map[string]string{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		out[entry.Name()] = string(raw)
	}
	require.NotEmpty(t, out)
	return out
}

// The committed pages must be the exact output of the generator. The second run writes into an
// empty directory, where a source the generator never reads is a missing page, not a stale one.
func TestTwoRunsOfTheGeneratorReproduceTheCommittedPages(t *testing.T) {
	committed := snapshotPages(t, filepath.Join(repoRoot(t), exampleEmbedDir))

	out, err := runMake(t, "docs")
	require.NoError(t, err, "make docs failed: %s", out)
	require.Equal(t, committed, snapshotPages(t, filepath.Join(repoRoot(t), exampleEmbedDir)),
		"make docs changed the committed pages")

	empty := t.TempDir()
	generated, err := runGenerator(t, "yaml", empty)
	require.NoError(t, err, "the generator failed: %s", generated)
	require.Equal(t, committed, snapshotPages(t, empty),
		"a run into an empty directory wrote a different set of pages")
}

// runGenerator starts the generator over one example directory and answers what it printed. The
// plugin that pulumi convert reads lives in the plugin directory of this repository, and no other.
func runGenerator(t *testing.T, exampleDir, markdownDir string) (string, error) {
	t.Helper()
	command := exec.Command("go", "run", "generate.go", exampleDir, markdownDir)
	command.Dir = filepath.Join(repoRoot(t), "docs")
	command.Env = append(os.Environ(),
		"PULUMI_HOME="+filepath.Join(repoRoot(t), ".pulumi"),
		"PULUMI_IGNORE_AMBIENT_PLUGINS=true",
		"PULUMI_DISABLE_AUTOMATIC_PLUGIN_ACQUISITION=true")
	out, err := command.CombinedOutput()
	return string(out), err
}

// brokenExampleSource names a property that the schema does not publish. The converter reports the
// property on the error stream, writes the program without it, and still exits with the code 0.
const brokenExampleSource = `name: broken
description: A source that names a property the schema does not publish
runtime: yaml
resources:
  broken-inbox:
    type: mailslurp:index:Inbox
    properties:
      name: broken-inbox
      notAPublishedProperty: value
`

// A dropped property leaves the resource count right, so the diagnostics guard is the only reader
// that catches it. This run fails when a reworded diagnostic stops the guard from firing.
func TestTheGeneratorRefusesASourceTheConverterReportsOn(t *testing.T) {
	// The generator needs the plugin that the docs target of the previous test installs.
	sources := t.TempDir()
	writeFile(t, filepath.Join(sources, "broken-examples.yaml"), brokenExampleSource)
	pages := t.TempDir()

	out, err := runGenerator(t, sources, pages)
	require.Error(t, err, "the generator accepted a source the converter reported on: %s", out)
	assert.Contains(t, out, "the conversion reported an error",
		"the generator stopped before it read the diagnostics: %s", out)

	entries, err := os.ReadDir(pages)
	require.NoError(t, err)
	assert.Empty(t, entries, "the generator wrote a page for a source it refused")
}
