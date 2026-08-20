package provider

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaEnum is one enum type of the schema: the member names and the wire values they carry.
type schemaEnum struct {
	names  []string
	values []string
}

// schemaEnums answers every enum type of the committed schema, keyed by its unqualified name.
func schemaEnums(t *testing.T) map[string]schemaEnum {
	t.Helper()
	var doc struct {
		Types map[string]struct {
			Enum []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"enum"`
		} `json:"types"`
	}
	require.NoError(t, json.Unmarshal(committedSchema(t), &doc))

	enums := map[string]schemaEnum{}
	for token, spec := range doc.Types {
		if len(spec.Enum) == 0 {
			continue
		}
		parts := strings.Split(token, ":")
		found := schemaEnum{}
		for _, element := range spec.Enum {
			found.names = append(found.names, element.Name)
			found.values = append(found.values, element.Value)
		}
		enums[parts[len(parts)-1]] = found
	}
	require.NotEmpty(t, enums, "the schema publishes no enum")
	return enums
}

// The Python generator turns an unnamed member into snake_case and back, so the all-caps wire
// value `HTTP_INBOX` comes out as `HTT_P_INBOX`. A member name of its own is the only lever.
func TestEveryEnumMemberCarriesAName(t *testing.T) {
	t.Parallel()
	for name, enum := range schemaEnums(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for i, member := range enum.names {
				assert.NotEmpty(t, member,
					"%s must name the member carrying %q rather than let each generator derive one",
					name, enum.values[i])
			}
		})
	}
}

// enumReference finds a member of one of this provider's enums in a generated program.
var enumReference = regexp.MustCompile(`mailslurp\.([A-Z][A-Za-z]*)\.([A-Za-z_][A-Za-z_0-9]*)`)

// The registry renders these pages, so a member spelled `RECEIVIN_G_EMAILS` is what a reader
// copies. Python appends an underscore to a keyword, and that suffix is the only legal difference.
func TestTheExamplePagesNamePythonEnumMembersByTheirValue(t *testing.T) {
	t.Parallel()
	enums := schemaEnums(t)
	seen := 0
	for _, stem := range exampleStems(t) {
		for _, block := range exampleBlocks(t, readEmbedFile(t, stem)) {
			for _, found := range enumReference.FindAllStringSubmatch(fenceBody(t, block, pythonLanguage), -1) {
				enum, ok := enums[found[1]]
				if !ok {
					continue
				}
				seen++
				assert.Contains(t, enum.values, strings.TrimSuffix(found[2], "_"),
					"%s shows %s.%s and the wire values are %v",
					stem, found[1], found[2], enum.values)
			}
		}
	}
	assert.NotZero(t, seen, "no example page uses an enum, so this check reads nothing")
}
