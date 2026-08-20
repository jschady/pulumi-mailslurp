package provider

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	indexPage        = "docs/_index.md"
	configurationPag = "docs/installation-configuration.md"
	logoFile         = "docs/logo.svg"
)

func registryPages() []string { return []string{indexPage, configurationPag} }

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), name))
	require.NoError(t, err, "%s must exist", name)
	return string(raw)
}

// frontMatter answers the text between the first two rulers of a page.
func frontMatter(t *testing.T, page string) string {
	t.Helper()
	body := readRepoFile(t, page)
	require.True(t, strings.HasPrefix(body, "---\n"), "%s must open with the front matter", page)
	end := strings.Index(body[4:], "\n---\n")
	require.GreaterOrEqual(t, end, 0, "%s must close the front matter", page)
	return body[4 : 4+end+1]
}

// The registry reads these three keys. A page without them renders as a plain file.
func TestBothRegistryPagesCarryTheFrontMatter(t *testing.T) {
	for _, page := range registryPages() {
		t.Run(page, func(t *testing.T) {
			matter := frontMatter(t, page)
			for _, key := range []string{"title:", "meta_desc:", "layout: package"} {
				assert.Contains(t, matter, key, "%s must carry %s", page, key)
			}
			for _, line := range strings.Split(strings.TrimSpace(matter), "\n") {
				name, value, found := strings.Cut(line, ":")
				assert.True(t, found, "%s carries a front matter line without a key: %q", page, line)
				assert.NotEmpty(t, strings.TrimSpace(value), "%s leaves %s empty", page, name)
			}
		})
	}
}

// fenceTagCount answers how many fenced blocks of each language one page opens.
func fenceTagCount(body string) map[string]int {
	counts := map[string]int{}
	open := false
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, fenceMarker) {
			continue
		}
		if open {
			open = false
			continue
		}
		open = true
		counts[strings.TrimSpace(strings.TrimPrefix(line, fenceMarker))]++
	}
	return counts
}

// A reader arrives in one language. The overview owes that reader one whole program, so the page
// carries one sample per SDK language and no more.
func TestTheIndexPageCarriesOneSamplePerLanguage(t *testing.T) {
	counts := fenceTagCount(readRepoFile(t, indexPage))
	for _, language := range exampleLanguages() {
		assert.Equal(t, 1, counts[language], "%s must carry one %s sample", indexPage, language)
	}
}

func TestEveryFencedBlockOfBothPagesCloses(t *testing.T) {
	for _, page := range registryPages() {
		t.Run(page, func(t *testing.T) {
			body := readRepoFile(t, page)
			assert.Equal(t, 0, strings.Count(body, "\n"+fenceMarker)%2,
				"%s opens a fenced block it never closes", page)
			for tag := range fenceTagCount(body) {
				assert.NotEmpty(t, tag, "%s opens a fenced block without a language", page)
			}
		})
	}
}

// publishedPackages are the names the SDKs carry on each registry.
func publishedPackages() map[string]string {
	return map[string]string{
		"npm":   "pulumi-mailslurp",
		"pypi":  "pulumi_mailslurp",
		"nuget": "Jschady.Mailslurp",
		"go":    "github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp",
	}
}

// The samples teach the reader what to install. A sample that names a package nobody can install
// costs the reader the first ten minutes of the provider.
func TestTheSamplesNameThePublishedPackages(t *testing.T) {
	body := readRepoFile(t, indexPage) + readRepoFile(t, configurationPag)
	for registry, name := range publishedPackages() {
		assert.Contains(t, body, name, "the pages must name the %s package", registry)
	}
	for _, wrong := range []string{"@jschady/mailslurp", "jschady_mailslurp", "@pulumi/mailslurp"} {
		assert.NotContains(t, body, wrong, "the pages must not name %s", wrong)
	}
	assert.NotContains(t, body, "https://github.com/jschady/pulumi-mailslurp/sdk",
		"the Go import path carries no scheme")
}

// installCommands maps each command the page tells a reader to run to the package it must name.
// PyPI distributes `pulumi-mailslurp`, and a program imports the same package as `pulumi_mailslurp`.
func installCommands() map[string]string {
	return map[string]string{
		"npm install":        publishedPackages()["npm"],
		"pip install":        "pulumi-mailslurp",
		"go get":             publishedPackages()["go"],
		"dotnet add package": publishedPackages()["nuget"],
	}
}

// A wrong name anywhere else costs the reader a paragraph. A wrong name here costs the reader the
// first command they run, so each command is read whole and not searched for in the page.
func TestEveryInstallCommandNamesItsPackage(t *testing.T) {
	body := readRepoFile(t, configurationPag)
	for command, want := range installCommands() {
		found := false
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, command+" ") {
				continue
			}
			found = true
			assert.Equal(t, command+" "+want, line, "the %s command must name %s", command, want)
		}
		assert.True(t, found, "the page must carry the %s command", command)
	}
}

// inboxSchema answers the input and the output property names of the inbox resource.
func inboxSchema(t *testing.T) (map[string]bool, map[string]bool) {
	t.Helper()
	var doc struct {
		Resources map[string]struct {
			InputProperties map[string]json.RawMessage `json:"inputProperties"`
			Properties      map[string]json.RawMessage `json:"properties"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &doc))
	inbox, ok := doc.Resources["mailslurp:index:Inbox"]
	require.True(t, ok, "the schema publishes no inbox")

	inputs := map[string]bool{}
	for name := range inbox.InputProperties {
		inputs[name] = true
	}
	outputs := map[string]bool{}
	for name := range inbox.Properties {
		outputs[name] = true
	}
	return inputs, outputs
}

// typescriptSample answers the text of the one TypeScript block of a page.
func typescriptSample(t *testing.T, page string) string {
	t.Helper()
	body := readRepoFile(t, page)
	open := fenceMarker + "typescript\n"
	start := strings.Index(body, open)
	require.GreaterOrEqual(t, start, 0, "%s carries no TypeScript sample", page)
	rest := body[start+len(open):]
	end := strings.Index(rest, "\n"+fenceMarker)
	require.GreaterOrEqual(t, end, 0, "the TypeScript sample never closes")
	return rest[:end]
}

// No SDK exists yet, so the samples are read against the committed schema and never compiled.
// A property the schema does not publish would reach the registry unread.
func TestTheIndexSampleSetsOnlyPublishedProperties(t *testing.T) {
	inputs, outputs := inboxSchema(t)
	sample := typescriptSample(t, indexPage)

	var written []string
	for _, line := range strings.Split(sample, "\n") {
		name, _, found := strings.Cut(strings.TrimSpace(line), ": ")
		if !found || strings.Contains(name, " ") || strings.HasPrefix(line, "const") {
			continue
		}
		written = append(written, name)
	}
	require.NotEmpty(t, written, "the sample sets no property")
	for _, name := range written {
		assert.True(t, inputs[name], "the sample sets %s, which the schema does not publish", name)
	}
	assert.True(t, outputs["emailAddress"], "the schema publishes no emailAddress output")
	assert.Contains(t, sample, "emailAddress", "the sample must export the address of the inbox")
}

// Every language sample creates the same inbox, so each one reads the same address back.
func TestEverySampleReadsTheAddressOfTheInbox(t *testing.T) {
	body := readRepoFile(t, indexPage)
	for _, spelling := range []string{"emailAddress", "email_address", "EmailAddress"} {
		assert.Contains(t, body, spelling, "a sample must read the address as %s", spelling)
	}
}

// The provider carries two update paths for a webhook, and only one of them sends the whole body.
// A page that reads the whole-body path as every update misreports what the other path keeps.
func TestThePageStatesWhatTheHeadersUpdateKeeps(t *testing.T) {
	source := readRepoFile(t, filepath.Join("provider", "internal", "webhook.go"))
	require.Contains(t, source, "client.UpdateWebhookHeaders(",
		"the provider must still carry the update that writes the headers alone")

	page := strings.Join(strings.Fields(readRepoFile(t, indexPage)), " ")
	assert.Contains(t, page, "An update of the headers alone keeps the transform",
		"the page must say what the update of the headers alone keeps")
	assert.NotContains(t, page, "Each update sends the whole webhook",
		"the page must not read every update as an update of the whole webhook")
}

// limitations maps each stated limitation to the text that states it. Each one is stated once:
// a reader who meets the same limitation twice cannot tell whether they are two problems.
func limitations() map[string]string {
	return map[string]string{
		"the shape of a 429 body":      "`retryAfterSeconds`",
		"the scope of the rate limit":  "150 requests each second",
		"a frozen account":             "`FROZEN`",
		"the inbox creation quota":     "publishes no quota",
		"the domain verification wait": "how long the wait is",
		"the free plan":                "publishes the limits of the free plan",
		"an inbox property you clear":  "the empty string",
		"the webhook basic access":     "`basicAuth`",
		"the account webhook conflict": "answers 409",
		"the webhook inbox":            "names the inbox on every update",
		"the transform of a webhook":   "`aiTransformId`",
		"the domain of the account":    "holds no custom domain",
		"the virtual inbox":            "`virtualInbox`",
		"the type of a domain":         "`domainType`",
		"the scope of a ruleset":       "`scope` and the `action`",
		"the forwarder entitlement":    "does not enable the inbox forwarder",
		"the forwarder comparison":     "`MATCH`",
		"the forwarder attachment":     "`attachmentTextExtractionMethod`",
		"the forwarder create body":    "the `field` and the `match`",
		"an output with no value":      "absent from the plan",
		"the address at preview":       "the address you set",
		"the python enum members":      "`AND_`",
		"the import of a property":     "the import cannot answer",
		"the protection of an import":  "refuses a protected",
		"the headers an import writes": "in plain text",
	}
}

func TestEveryLimitationIsStatedOnce(t *testing.T) {
	var body string
	for _, page := range registryPages() {
		body += readRepoFile(t, page)
	}
	// A statement that wraps over two lines is one statement, so the count reads the words.
	body = strings.Join(strings.Fields(body), " ")

	for item, marker := range limitations() {
		assert.Equal(t, 1, strings.Count(body, marker),
			"the pages must state %s once, with %q", item, marker)
	}
}

// The README and the pages have different readers. Text that lives in both goes stale in one.
func TestTheReadmeRepeatsNoSentenceOfThePages(t *testing.T) {
	var pages string
	for _, page := range registryPages() {
		pages += readRepoFile(t, page)
	}
	for _, line := range strings.Split(readRepoFile(t, "README.md"), "\n") {
		line = strings.TrimSpace(line)
		if len(strings.Fields(line)) < 8 {
			continue
		}
		assert.NotContains(t, pages, line, "the README repeats a line of the pages")
	}
}

// forbiddenWords answers the list of words that the contribution page keeps out of the shipped
// text. The list lives in that page, so one edit moves both the rule and the check.
func forbiddenWords(t *testing.T) []string {
	t.Helper()
	const heading = "### The words that never appear"
	body := readRepoFile(t, "CONTRIBUTING.md")
	start := strings.Index(body, heading)
	require.GreaterOrEqual(t, start, 0, "the contribution page must carry the list")

	var words []string
	for _, line := range strings.Split(body[start+len(heading):], "\n") {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if word, found := strings.CutPrefix(line, "- "); found {
			words = append(words, strings.ToLower(strings.Trim(strings.TrimSpace(word), "`")))
		}
	}
	require.Greater(t, len(words), 10, "the list must carry the words it names")
	return words
}

// shippedProse answers every user-facing text of this repository, with the code taken out.
func shippedProse(t *testing.T) map[string]string {
	t.Helper()
	fences := regexp.MustCompile("(?s)```.*?```")
	spans := regexp.MustCompile("`[^`]*`")

	prose := map[string]string{}
	for _, page := range append(registryPages(), "README.md") {
		prose[page] = spans.ReplaceAllString(fences.ReplaceAllString(readRepoFile(t, page), " "), " ")
	}

	var descriptions []string
	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if text, ok := child.(string); ok && key == descriptionKey {
					descriptions = append(descriptions, text)
					continue
				}
				walk(child)
			}
		case []any:
			for _, item := range value {
				walk(item)
			}
		}
	}
	var document any
	require.NoError(t, json.Unmarshal(committedSchema(t), &document))
	walk(document)
	require.NotEmpty(t, descriptions)
	joined := strings.Join(descriptions, "\n")
	prose[committedSchemaFile] = spans.ReplaceAllString(fences.ReplaceAllString(joined, " "), " ")
	return prose
}

// The glossary is prose until something reads it. This reads it: one concept keeps one word
// across the pages, the README, and every description the registry renders.
func TestTheShippedProseKeepsToTheGlossary(t *testing.T) {
	words := forbiddenWords(t)
	for name, prose := range shippedProse(t) {
		t.Run(name, func(t *testing.T) {
			text := strings.ToLower(prose)
			for _, word := range words {
				pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
				assert.False(t, pattern.MatchString(text),
					"%s carries %q, which the glossary replaces", name, word)
			}
		})
	}
}

// workflowJobs answers the job identifiers of one workflow file.
func workflowJobs(t *testing.T, name string) []string {
	t.Helper()
	body := readRepoFile(t, filepath.Join(".github", "workflows", name))
	jobs, found := strings.CutPrefix(body[strings.Index(body, "\njobs:\n"):], "\njobs:\n")
	require.True(t, found, "%s declares no job", name)

	var identifiers []string
	for _, line := range strings.Split(jobs, "\n") {
		id, isJob := strings.CutSuffix(line, ":")
		if !isJob || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		identifiers = append(identifiers, strings.TrimSpace(id))
	}
	require.NotEmpty(t, identifiers, "%s declares no job", name)
	return identifiers
}

// The page that describes the workflows went stale once: it named neither the workflow linter nor
// the prose check. A page that names every job and every pinned tool cannot go stale unread.
func TestTheWorkflowPageNamesEveryJobAndEveryPinnedTool(t *testing.T) {
	page := readRepoFile(t, filepath.Join(".github", "workflows", "README.md"))

	for _, workflow := range []string{"pull-request.yml", "main.yml", "release.yml", "spec-drift.yml"} {
		t.Run(workflow, func(t *testing.T) {
			assert.Contains(t, page, "`"+workflow+"`", "the page must name %s", workflow)
			for _, job := range workflowJobs(t, workflow) {
				assert.Contains(t, page, "`"+job+"`", "the page must name the %s job of %s", job, workflow)
			}
		})
	}

	for _, tool := range []string{"actionlint", "make lint_prose", "check-workflows.sh",
		"check-internal-references.sh"} {
		assert.Contains(t, page, tool, "the page must name %s, which the lint job runs", tool)
	}
}

// The registry fetches the logo over the network, so the file must carry the whole drawing.
func TestTheLogoIsOneSelfContainedDrawing(t *testing.T) {
	body := readRepoFile(t, logoFile)
	var svg struct {
		ViewBox string `xml:"viewBox,attr"`
	}
	require.NoError(t, xml.Unmarshal([]byte(body), &svg), "the logo must parse as XML")
	assert.NotEmpty(t, svg.ViewBox, "the logo must carry a viewBox, so it scales to any size")

	// The namespace is the one address the drawing carries. Anything else it fetches is a file
	// the registry does not serve.
	for _, external := range []string{"<image", "xlink:href", "href=", "<script", "<use"} {
		assert.NotContains(t, body, external, "the logo must hold no reference to another file")
	}
}

func TestTheSchemaPointsAtTheCommittedLogo(t *testing.T) {
	var doc struct {
		LogoURL string `json:"logoUrl"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &doc))
	assert.True(t, strings.HasSuffix(doc.LogoURL, "/"+logoFile),
		"the schema must point at %s, and it points at %s", logoFile, doc.LogoURL)
}

// The prose check greps the tracked files for the name of the writing standard. This reads the
// same list with its own scan, so a hole in the check cannot hide a page that names the standard.
func TestNoTrackedFileNamesTheWritingStandard(t *testing.T) {
	command := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	command.Dir = repoRoot(t)
	listed, err := command.Output()
	require.NoError(t, err)

	names := strings.Fields(string(listed))
	require.Greater(t, len(names), 20, "the scan must read the whole repository")

	// Each needle is built here, so this file carries no name it forbids everywhere else.
	short := strings.ToUpper("ste")
	needles := []string{short + "-100", short + "100",
		"simplified" + " technical " + "english", "controlled" + " english"}
	read := 0
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), name))
		if err != nil {
			continue
		}
		read++
		text := strings.ToLower(string(raw))
		for _, needle := range needles {
			assert.NotContains(t, text, strings.ToLower(needle), "%s names the writing standard", name)
		}
	}
	require.Greater(t, read, 20, "the scan must open the files it lists")
}
