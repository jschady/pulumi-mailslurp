package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The committed MailSlurp API document is the oracle for every vendor value the provider ships.
// Reading it at test time keeps a second hand-written list from drifting away from the vendor.

// pinnedSpecPath is the committed copy of the MailSlurp API document, from the repository root.
var pinnedSpecPath = filepath.Join("..", "..", "api", "openapi.json")

// specEnumValues answers the values the pinned API spec declares for one property. The schema is
// checked against the vendor document, so no second hand-written list can drift from it.
func specEnumValues(t *testing.T, schemaName, property string) []string {
	t.Helper()
	raw, err := os.ReadFile(pinnedSpecPath)
	require.NoError(t, err)

	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	values := doc.Components.Schemas[schemaName].Properties[property].Enum
	require.NotEmpty(t, values, "the pinned spec declares no enum for %s.%s", schemaName, property)
	return values
}

// clientOperationNames answers every operation name the client source carries. The client sends
// one of these strings with each call, so the set is what the provider asks the vendor for.
func clientOperationNames(t *testing.T) []string {
	t.Helper()
	sources, err := filepath.Glob("client*.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources, "the scan read no client source")

	pattern := regexp.MustCompile(`operation:\s*"(\w+)"`)
	seen := map[string]bool{}
	var names []string
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		//nolint:gosec // G304: the path comes from a glob over this package's own directory.
		raw, err := os.ReadFile(source)
		require.NoError(t, err)
		for _, found := range pattern.FindAllStringSubmatch(string(raw), -1) {
			if seen[found[1]] {
				continue
			}
			seen[found[1]] = true
			names = append(names, found[1])
		}
	}
	sort.Strings(names)
	return names
}

// specOperationIDs answers every operationId the pinned spec declares under its paths.
func specOperationIDs(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(pinnedSpecPath)
	require.NoError(t, err)

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	ids := map[string]bool{}
	for _, methods := range doc.Paths {
		for _, body := range methods {
			var operation struct {
				OperationID string `json:"operationId"`
			}
			// A path can carry a value that is no operation, such as a shared parameter list.
			if err := json.Unmarshal(body, &operation); err != nil || operation.OperationID == "" {
				continue
			}
			ids[operation.OperationID] = true
		}
	}
	return ids
}

// TestEveryOperationTheClientNamesIsInTheSpec compares the two lists. A vendor that renames an
// operation the client calls leaves the name in the client with nothing behind it.
func TestEveryOperationTheClientNamesIsInTheSpec(t *testing.T) {
	names := clientOperationNames(t)
	require.NotEmpty(t, names, "the client source names no operation")

	ids := specOperationIDs(t)
	require.NotEmpty(t, ids, "the pinned spec declares no operationId")

	for _, name := range names {
		require.Truef(t, ids[name],
			"the client calls %q and the pinned spec declares no operationId of that name", name)
	}
}
