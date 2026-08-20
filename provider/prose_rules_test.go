package provider

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	proseScript    = "scripts/lint-prose.sh"
	descriptionKey = "description"
)

// proseCheck runs the prose check over one seeded page and answers what it printed.
func proseCheck(t *testing.T, body string) (string, error) {
	t.Helper()
	seed := filepath.Join(t.TempDir(), "seed.md")
	writeFile(t, seed, body)
	return runMake(t, "lint_prose", "PROSE_FILES="+seed)
}

// proseSandbox builds a repository that holds the real check and nothing else, so a test can
// give the check a whole tree without touching this one.
func proseSandbox(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "scripts"), 0o750))
	script, err := os.ReadFile(filepath.Join(repoRoot(t), proseScript))
	require.NoError(t, err)
	copied := filepath.Join(root, proseScript)
	//nolint:gosec // G703: the path is the temporary directory of this test, and nothing else.
	require.NoError(t, os.WriteFile(copied, script, 0o600))
	require.NoError(t, os.Chmod(copied, 0o700))
	return root
}

func runInSandbox(t *testing.T, root string) (string, error) {
	t.Helper()
	command := exec.Command("./" + proseScript)
	command.Dir = root
	out, err := command.CombinedOutput()
	return string(out), err
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	command := exec.Command("git", "init", "-q", ".")
	command.Dir = root
	out, err := command.CombinedOutput()
	require.NoError(t, err, string(out))
}

// A comma chain of three items is a list that lost its bullets, and the check never read one.
func TestTheProseCheckRejectsACommaChainOfThreeItems(t *testing.T) {
	out, err := proseCheck(t, "# The page\n\nThe provider creates the inbox, the webhook, and the template.\n")
	require.Error(t, err, "the check accepted a comma chain of three items: %s", out)
	require.Contains(t, out, ":3:", "the check must name the line of the comma chain: %s", out)
}

// A chain of property names is code, and code carries no writing rules. The registry pages and
// both READMEs already carry such chains on purpose.
func TestTheProseCheckAcceptsACommaChainOfIdentifiers(t *testing.T) {
	out, err := proseCheck(t, "# The page\n\nYou can set the `name`, the `tags`, and the `favourite` properties.\n")
	require.NoError(t, err, "the check read a chain of identifiers as prose: %s", out)
}

// Two sentences of the same length, one a step and one a statement: only the step is over its
// limit. A single limit for both would fail this test on the statement too.
func TestTheProseCheckHoldsAStepToTheShorterLimit(t *testing.T) {
	long := "The provider" + strings.Repeat(" and the inbox", 7) + " work together"

	out, err := proseCheck(t, "# The page\n\n1. "+long+".\n")
	require.Error(t, err, "the check accepted a step of 23 words: %s", out)
	require.Contains(t, out, "the limit is 20 words", "the step carries the procedural limit: %s", out)

	out, err = proseCheck(t, "# The page\n\n"+long+".\n")
	require.NoError(t, err, "the check read a statement of 23 words as a step: %s", out)
}

// Every step of a list is its own instruction, so the report must name the step that is too long.
func TestTheProseCheckNamesTheStepThatIsTooLong(t *testing.T) {
	long := "Run the command" + strings.Repeat(" and the next command", 6)
	body := "# The page\n\n1. Set the API key.\n2. " + long + ".\n3. Read the output.\n"

	out, err := proseCheck(t, body)
	require.Error(t, err, "the check accepted a step over the limit: %s", out)
	require.Contains(t, out, ":4:", "the check must name the line of the long step: %s", out)
}

// The check skipped every heading, so a heading could carry a banned length and reach the registry.
func TestTheProseCheckReadsAHeading(t *testing.T) {
	out, err := proseCheck(t, "# The heading"+strings.Repeat(" of the inbox", 9)+"\n\nThe provider creates the inbox.\n")
	require.Error(t, err, "the check skipped a heading of 28 words: %s", out)
	require.Contains(t, out, ":1:", "the check must name the line of the heading: %s", out)
}

// Reference data lives in tables, so a table cell holds shipped prose the check never read.
func TestTheProseCheckReadsATableCell(t *testing.T) {
	cell := "The key that the provider sends" + strings.Repeat(" to the API of the account", 4)
	body := "# The page\n\n| Property | What it is |\n| --- | --- |\n| `apiKey` | " + cell + ". |\n"

	out, err := proseCheck(t, body)
	require.Error(t, err, "the check skipped a table cell of 31 words: %s", out)
	require.Contains(t, out, ":5:", "the check must name the line of the table row: %s", out)
}

// A table cell that reads as a short list stays a cell. A list inside a cell reads worse.
func TestTheProseCheckAcceptsACommaChainInATableCell(t *testing.T) {
	body := "# The page\n\n| Property | What it is |\n| --- | --- |\n" +
		"| `state` | The inbox, the webhook, and the template. |\n"

	out, err := proseCheck(t, body)
	require.NoError(t, err, "the check read a table cell as a paragraph: %s", out)
}

// The whole-repository scan reads the file list from git. Where git answers nothing the scan
// found nothing to read, and the check printed a pass it had not earned.
func TestTheProseCheckRefusesToRunWhereItCannotListTheFiles(t *testing.T) {
	root := proseSandbox(t)
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check passed where it could not list the files: %s", out)
	require.NotContains(t, out, "The prose check passed.")
	// The two refusals have two causes and two remedies, so each one says which it met.
	require.Contains(t, out, "cannot list the files", "the check must say what it could not do: %s", out)
}

// A run that reads no file at all is the same false pass, one directory further along.
func TestTheProseCheckRefusesATreeWithNoProse(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check passed on a tree that holds no prose: %s", out)
	require.NotContains(t, out, "The prose check passed.")
	require.Contains(t, out, "no page and no schema", "the check must say what it did not find: %s", out)
}

// A tree can hide every file from git and still hold a page on disk. The scan for the name of the
// writing standard then reads nothing, which is the false pass one refusal further along.
func TestTheProseCheckRefusesATreeThatHidesEveryFileFromGit(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)
	writeFile(t, filepath.Join(root, ".gitignore"), "*\n")
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check passed where git listed no file: %s", out)
	require.NotContains(t, out, "The prose check passed.")
	require.Contains(t, out, "no file to read", "the check must say what it did not find: %s", out)
}

// The contribution page is shipped prose, and the default run never read it.
func TestTheProseCheckReadsTheContributionPage(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")
	writeFile(t, filepath.Join(root, "CONTRIBUTING.md"), "# The page\n\nYou can utilize the provider.\n")

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check skipped the contribution page: %s", out)
	require.Contains(t, out, "CONTRIBUTING.md", "the check must name the page it rejected: %s", out)
}

// The page that describes the workflows is prose the maintainer reads, and the default run
// never opened it.
func TestTheProseCheckReadsTheWorkflowPage(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o750))
	writeFile(t, filepath.Join(root, ".github", "workflows", "README.md"),
		"# The page\n\nYou can utilize the workflow.\n")

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check skipped the workflow page: %s", out)
	require.Contains(t, out, "workflows/README.md", "the check must name the page it rejected: %s", out)
}

// proseDefaultPages answers the pages the check reads when it runs with no argument.
func proseDefaultPages(t *testing.T) []string {
	t.Helper()
	const marker = "\tfor page in "
	script := readRepoFile(t, proseScript)
	start := strings.Index(script, marker)
	require.GreaterOrEqual(t, start, 0, "the check must still name its default pages")
	line, _, found := strings.Cut(script[start+len(marker):], ";")
	require.True(t, found, "the check must still loop over its default pages")

	pages := strings.Fields(line)
	require.Greater(t, len(pages), 1, "the check must read more than one page")
	return pages
}

// The README tells a maintainer what the prose check reads. The check owns that list, so a page
// added there and left out here sends the maintainer a list that is short.
func TestTheReadmeNamesEveryDefaultPageOfTheProseCheck(t *testing.T) {
	body := readRepoFile(t, "README.md")
	for _, page := range proseDefaultPages(t) {
		if page == "README.md" {
			continue // The README calls itself "this page".
		}
		assert.Contains(t, body, "`"+page+"`", "the README must name %s, which the check reads", page)
	}
}

// plainWord strips the regular-expression atoms from one term of either list.
func plainWord(term string) string {
	return strings.TrimSpace(strings.NewReplacer(
		"(?i)", "", `\b`, "", "(", "", ")", "", `\`, "", "`", "").Replace(term))
}

// quoted answers the text inside every pair of the given mark.
func quoted(text, mark string) []string {
	var found []string
	pairs := regexp.MustCompile(mark+"([^"+mark+"]*)"+mark).FindAllStringSubmatch(text, -1)
	for _, pair := range pairs {
		found = append(found, pair[1])
	}
	return found
}

// bannedTerms answers the words that one file refuses, from its own source.
func bannedTerms(t *testing.T, file, from, to string) map[string]bool {
	t.Helper()
	body := readRepoFile(t, file)
	start := strings.Index(body, from)
	require.GreaterOrEqual(t, start, 0, "%s carries no %q", file, from)
	end := strings.Index(body[start:], to)
	require.Greater(t, end, 0, "%s carries no %q after %q", file, to, from)

	terms := map[string]bool{}
	block := body[start : start+end]
	for _, mark := range []string{"`", `"`, "'"} {
		for _, segment := range quoted(block, mark) {
			for _, term := range strings.Split(plainWord(segment), "|") {
				if word := plainWord(term); word != "" {
					terms[word] = true
				}
			}
		}
	}
	return terms
}

// The schema test held words the check did not, so a description could pass one reader and fail
// the other. The check carries the wider list now, and this holds it there.
func TestTheProseCheckBansEveryWordTheSchemaTestBans(t *testing.T) {
	script := bannedTerms(t, proseScript, "BANNED_WORDS=", "STANDARD_PATTERN=")
	require.Greater(t, len(script), 40, "the check must carry its list")

	schema := bannedTerms(t, filepath.Join("provider", "internal", "provider_test.go"),
		"var bannedWords", "func TestNoDescriptionUsesABannedWord")
	require.Greater(t, len(schema), 30, "the schema test must carry its list")

	for word := range schema {
		assert.True(t, script[word], "the prose check must refuse %q, which the schema test refuses", word)
	}
}

// seedSchema writes a schema whose one long description carries a banned word, and answers the
// line of that description in the file.
func seedSchema(t *testing.T, root string) int {
	t.Helper()
	dir := filepath.Join(root, "provider", "cmd", "pulumi-resource-seed")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	filler := strings.Repeat("The provider reads the inbox.\n", 40)
	document := map[string]any{
		"name":         "seed",
		descriptionKey: "The seeded provider.",
		"resources": map[string]any{
			"seed:index:A": map[string]any{descriptionKey: "A resource.\n" + filler + "You can utilize it."},
		},
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(path, append(raw, '\n'), 0o600))

	for number, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "utilize") {
			return number + 1
		}
	}
	t.Fatal("the seeded schema carries no banned word")
	return 0
}

// A finding that names a line of the extracted stream sends the reader to the wrong line of the
// schema. The line of the description in the file is the one a reader can open.
func TestTheProseCheckNamesTheSchemaLineOfADescription(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")
	line := seedSchema(t, root)

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check accepted a banned word in a description: %s", out)
	require.Contains(t, out, "schema.json:"+strconv.Itoa(line)+":",
		"the check must name line %d, where the description is: %s", line, out)
}

// The line of a description comes from the shape of the file. A schema on one line carries no
// line to report, and a silent pass would hide every description of that schema.
func TestTheProseCheckRefusesASchemaOnOneLine(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")

	dir := filepath.Join(root, "provider", "cmd", "pulumi-resource-seed")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	document := `{"name":"seed","resources":{"seed:index:A":{"description":"A resource."}}}`
	writeFile(t, filepath.Join(dir, "schema.json"), document+"\n")

	out, err := runInSandbox(t, root)
	require.Error(t, err, "the check read a schema whose lines it cannot name: %s", out)
	require.Contains(t, out, "one key per line", "the check must say what to do: %s", out)
}

// Two descriptions that meet in the stream must not read as one sentence.
func TestTheProseCheckReadsEachDescriptionApart(t *testing.T) {
	root := proseSandbox(t)
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "README.md"), "# The page\n\nThe provider creates the inbox.\n")

	dir := filepath.Join(root, "provider", "cmd", "pulumi-resource-seed")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	half := "The provider" + strings.Repeat(" and the inbox", 6)
	document := map[string]any{
		"name": "seed",
		"resources": map[string]any{
			"seed:index:A": map[string]any{descriptionKey: half},
			"seed:index:B": map[string]any{descriptionKey: half + " work together."},
		},
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.json"), append(raw, '\n'), 0o600))

	out, err := runInSandbox(t, root)
	require.NoError(t, err, "the check joined two descriptions into one sentence: %s", out)
}
