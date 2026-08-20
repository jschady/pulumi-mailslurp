package internal

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "github.com/pulumi/pulumi-go-provider"
)

// schemaItems is the element type of a list property. A named element type is a reference.
type schemaItems struct {
	Ref string `json:"$ref"`
}

type schemaProperty struct {
	Description      string       `json:"description"`
	Secret           bool         `json:"secret"`
	ReplaceOnChanges bool         `json:"replaceOnChanges"`
	Items            *schemaItems `json:"items"`
	DefaultInfo      *struct {
		Environment []string `json:"environment"`
	} `json:"defaultInfo"`
}

type schemaResource struct {
	Description     string                     `json:"description"`
	Properties      map[string]*schemaProperty `json:"properties"`
	InputProperties map[string]*schemaProperty `json:"inputProperties"`
	RequiredInputs  []string                   `json:"requiredInputs"`
}

type schemaEnumValue struct {
	Value       string `json:"value"`
	Description string `json:"description"`
}

// schemaType is a named type of the schema: an enum carries values, an object carries members and
// the list of the ones the API requires.
type schemaType struct {
	Type       string                     `json:"type"`
	Enum       []schemaEnumValue          `json:"enum"`
	Properties map[string]*schemaProperty `json:"properties"`
	Required   []string                   `json:"required"`
}

type schemaDoc struct {
	Name   string `json:"name"`
	Config struct {
		Variables map[string]*schemaProperty `json:"variables"`
	} `json:"config"`
	Resources map[string]*schemaResource `json:"resources"`
	Types     map[string]*schemaType     `json:"types"`
	Provider  struct {
		InputProperties map[string]*schemaProperty `json:"inputProperties"`
	} `json:"provider"`
	DisplayName       string   `json:"displayName"`
	Description       string   `json:"description"`
	Keywords          []string `json:"keywords"`
	Homepage          string   `json:"homepage"`
	License           string   `json:"license"`
	Publisher         string   `json:"publisher"`
	Namespace         string   `json:"namespace"`
	Repository        string   `json:"repository"`
	PluginDownloadURL string   `json:"pluginDownloadURL"`
	LogoURL           string   `json:"logoUrl"`
}

// The two lists are the Inbox diff contract; every diff test reads them.
var (
	replaceForcing = []string{
		"emailAddress", "domainName", "domainId", "inboxType",
		"useDomainPool", "useShortAddress", "virtualInbox", "prefix", "expiresIn",
	}
	updatable = []string{"name", "description", "tags", "expiresAt", "favourite"}
)

func getSchema(t *testing.T) schemaDoc {
	t.Helper()
	s := newTestServer(t, nil)
	resp, err := s.GetSchema(p.GetSchemaRequest{Version: 0})
	require.NoError(t, err)
	var doc schemaDoc
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &doc))
	return doc
}

// schemaObjectType answers one object type of the schema. A named type the schema does not carry,
// or one that is not an object, fails here rather than reading as an object with no member.
func schemaObjectType(t *testing.T, name string) *schemaType {
	t.Helper()
	ty := getSchema(t).Types[name]
	require.NotNil(t, ty, "the schema exposes no %s", name)
	assert.Equal(t, "object", ty.Type)
	return ty
}

// assertEnumValues checks one enum the schema publishes against the values it must carry, and
// insists that every value ships a description the registry can print.
func assertEnumValues(t *testing.T, token string, want []string) {
	t.Helper()
	enum := getSchema(t).Types[token]
	require.NotNil(t, enum, "the schema exposes no %s", token)
	assert.Equal(t, "string", enum.Type)

	got := map[string]string{}
	for _, v := range enum.Enum {
		got[v.Value] = v.Description
	}
	assert.Len(t, got, len(want))
	for _, value := range want {
		assert.Contains(t, got, value)
		assert.NotEmpty(t, got[value], "enum value %q has no description", value)
	}
}

func TestReplaceForcingInboxPropertiesCarryTheQuotaWarning(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	inbox := schema.Resources["mailslurp:index:Inbox"]
	require.NotNil(t, inbox)

	for _, name := range replaceForcing {
		prop := inbox.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.Contains(t, prop.Description, "Warning:", "property %q", name)
		assert.Contains(t, prop.Description, "quota", "property %q", name)
	}
	for _, name := range updatable {
		prop := inbox.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.NotContains(t, prop.Description, "Warning:", "property %q", name)
	}
}

func TestInboxDoesNotModelAllowTeamAccess(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	inbox := schema.Resources["mailslurp:index:Inbox"]
	require.NotNil(t, inbox)
	assert.NotContains(t, inbox.InputProperties, "allowTeamAccess")
	assert.NotContains(t, inbox.Properties, "allowTeamAccess")
}

func TestInboxTagsTheNineReplaceForcingProperties(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	inbox := schema.Resources["mailslurp:index:Inbox"]
	require.NotNil(t, inbox)
	require.Len(t, inbox.InputProperties, len(replaceForcing)+len(updatable))

	for _, name := range replaceForcing {
		prop := inbox.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.True(t, prop.ReplaceOnChanges, "property %q must force a replacement", name)
	}
	for _, name := range updatable {
		prop := inbox.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.False(t, prop.ReplaceOnChanges, "property %q must update in place", name)
	}
}

func TestInboxStateCoversEveryInboxInput(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	inbox := schema.Resources["mailslurp:index:Inbox"]
	require.NotNil(t, inbox)
	require.NotEmpty(t, inbox.InputProperties)
	for name := range inbox.InputProperties {
		assert.Contains(t, inbox.Properties, name,
			"input property %q has no output of the same name, so the diff cannot read it from the state", name)
	}
}

// The values come from the pinned API spec at test time, so a vendor value the spec grows fails
// here instead of reaching a stack outside the enum.
func TestInboxTypeEnumUsesTheVerbatimAPIValues(t *testing.T) {
	t.Parallel()
	want := specEnumValues(t, "CreateInboxDto", inboxPropInboxType)
	// One Go type serves the input and the output, which holds while both spec schemas agree.
	assert.Equal(t, want, specEnumValues(t, "InboxDto", inboxPropInboxType))
	assertEnumValues(t, "mailslurp:index:InboxType", want)
}

func TestSchemaCarriesTheRegistryMetadata(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	assert.Equal(t, "MailSlurp", schema.DisplayName)
	assert.Equal(t, "jschady", schema.Publisher)
	assert.Equal(t, "jschady", schema.Namespace)
	assert.Equal(t, "Apache-2.0", schema.License)
	assert.Equal(t, "https://www.mailslurp.com", schema.Homepage)
	assert.Equal(t, "https://github.com/jschady/pulumi-mailslurp", schema.Repository)
	assert.Equal(t, "github://api.github.com/jschady/pulumi-mailslurp", schema.PluginDownloadURL)
	assert.Equal(t, []string{"pulumi", "mailslurp", "email", "category/utility", "kind/native"}, schema.Keywords)
	assert.NotEmpty(t, schema.LogoURL)
	assert.NotEmpty(t, schema.Description)
}

// A declaration that gains a neighbour directly above it takes over the comment that described
// the declaration below, which leaves that one undocumented and this one described as something else.
func TestEveryTypeAndVariableCarriesTheCommentThatNamesIt(t *testing.T) {
	t.Parallel()
	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources)

	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ParseComments)
		require.NoError(t, err)
		for _, declaration := range file.Decls {
			names, doc := declaredNames(declaration)
			if doc == "" || len(names) == 0 {
				continue
			}
			assert.Contains(t, names, strings.Fields(doc)[0],
				"%s: the comment %q sits above %v, so it describes the wrong declaration", source, doc, names)
		}
	}
}

// declaredNames answers the names one type or variable declaration binds, and its first comment line.
func declaredNames(declaration ast.Decl) ([]string, string) {
	general, ok := declaration.(*ast.GenDecl)
	if !ok || general.Doc == nil || (general.Tok != token.TYPE && general.Tok != token.VAR) {
		return nil, ""
	}
	var names []string
	for _, spec := range general.Specs {
		switch declared := spec.(type) {
		case *ast.TypeSpec:
			names = append(names, declared.Name.Name)
		case *ast.ValueSpec:
			for _, name := range declared.Names {
				names = append(names, name.Name)
			}
		}
	}
	first := strings.TrimSpace(strings.TrimPrefix(general.Doc.List[0].Text, "//"))
	if first == "" {
		return nil, ""
	}
	return names, first
}

// withoutExamples drops the generated example block. It carries code and the headings that the
// generator writes, and the example sources check that prose on their own.
func withoutExamples(text string) string {
	if at := strings.Index(text, "{{% examples %}}"); at >= 0 {
		return strings.TrimSpace(text[:at])
	}
	return text
}

// schemaDescriptions returns every hand-written string the schema ships, keyed by its location.
func schemaDescriptions(t *testing.T) map[string]string {
	t.Helper()
	schema := getSchema(t)
	out := map[string]string{"provider": schema.Description}
	for name, v := range schema.Config.Variables {
		out["config."+name] = v.Description
	}
	for token, r := range schema.Resources {
		out[token] = r.Description
		for name, prop := range r.InputProperties {
			out[token+".in."+name] = prop.Description
		}
		for name, prop := range r.Properties {
			out[token+".out."+name] = prop.Description
		}
	}
	for token, ty := range schema.Types {
		for _, v := range ty.Enum {
			out[token+".enum."+v.Value] = v.Description
		}
	}
	for where, text := range out {
		out[where] = withoutExamples(text)
	}
	return out
}

var backticked = regexp.MustCompile("`[^`]*`")

// splitSentences ends a sentence at terminal punctuation that a space or the string end follows.
func splitSentences(text string) []string {
	flat := strings.Join(strings.Fields(text), " ")
	var out []string
	start := 0
	for i := range len(flat) {
		if flat[i] != '.' && flat[i] != '!' && flat[i] != '?' {
			continue
		}
		if i+1 < len(flat) && flat[i+1] != ' ' {
			continue
		}
		if s := strings.TrimSpace(flat[start : i+1]); s != "" {
			out = append(out, s)
		}
		start = i + 1
	}
	if s := strings.TrimSpace(flat[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

const descriptiveWordLimit = 25

func TestEveryDescriptionSentenceStaysInsideTheWordLimit(t *testing.T) {
	t.Parallel()
	all := schemaDescriptions(t)
	require.NotEmpty(t, all)
	for where, text := range all {
		require.NotEmpty(t, text, "%s has no description", where)
		for _, sentence := range splitSentences(text) {
			words := strings.Fields(backticked.ReplaceAllString(sentence, "X"))
			assert.LessOrEqual(t, len(words), descriptiveWordLimit,
				"%s: sentence of %d words: %s", where, len(words), sentence)
		}
	}
}

// bannedWords are the replaced forms of the writing card; the schema must use the replacement.
var bannedWords = regexp.MustCompile(`(?i)\b(utilize|utilizes|leverage|leverages|employs|via|ensure|` +
	`ensures|may|might|functionality|capabilities|i\.e\.|e\.g\.|etc\.|please|simply|just|easily|merely|` +
	`desired|aforementioned|respectively|powerful|seamless|robust|blazing|effortless|` +
	`field|fields|attribute|attributes|token|tokens|credential|credentials)\b`)

var bannedPhrases = []string{
	"in order to", "prior to", "subsequent to", "in the event that", "at this time",
	"a number of", "allows you to", "enables you to", "note that", "in terms of", "with respect to",
}

func TestNoDescriptionUsesABannedWord(t *testing.T) {
	t.Parallel()
	all := schemaDescriptions(t)
	require.NotEmpty(t, all)
	for where, text := range all {
		prose := backticked.ReplaceAllString(text, "X")
		assert.Empty(t, bannedWords.FindAllString(prose, -1), "%s uses a banned word", where)
		for _, phrase := range bannedPhrases {
			assert.NotContains(t, strings.ToLower(prose), phrase, "%s uses a banned phrase", where)
		}
	}
}

// allowedOpeners keep the first sentence on an article, a condition, the reader, or a named actor.
var allowedOpeners = []string{"The", "A", "An", "If", "You", "Each", "MailSlurp", "Pulumi"}

func TestEveryDescriptionOpensWithTheThingOrTheActor(t *testing.T) {
	t.Parallel()
	all := schemaDescriptions(t)
	require.NotEmpty(t, all)
	for where, text := range all {
		for _, line := range strings.Split(text, "\n") {
			assert.Equal(t, strings.TrimLeft(line, " \t"), line, "%s indents a line, which markdown renders as code", where)
		}
		sentences := splitSentences(text)
		require.NotEmpty(t, sentences, "%s has no sentence", where)
		first := strings.Fields(sentences[0])[0]
		assert.Contains(t, allowedOpeners, strings.Trim(first, "*"),
			"%s drops the subject: %s", where, sentences[0])
	}
}
