package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
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
