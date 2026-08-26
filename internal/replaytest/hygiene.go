package replaytest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cassetteSuffix is the extension the recorder gives every cassette.
const cassetteSuffix = ".yaml"

// scrubbedHeaderLine matches a header a scrubbed cassette never carries. A cassette writes each
// header as a YAML key, so the match starts at the head of a line and never reads a body.
var scrubbedHeaderLine = regexp.MustCompile(`(?im)^[ \t-]*(x-api-key|authorization|cookie|set-cookie)[ \t]*:`)

// keyAfterTheHeader matches a MailSlurp key, which is a UUID, next to the name of its header.
// The name must be close by, so an identifier in a body reads as the UUID it is.
var keyAfterTheHeader = regexp.MustCompile(
	`(?is)x-api-key.{0,60}?[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// secretMemberValue matches a JSON member that names a credential together with its string value.
// A cassette holds a body as one YAML scalar, and the quotes of the JSON survive it, so the value
// follows the name. The optional backslash reads a body a double-quoted scalar escaped. The names
// come from SecretMembers, so the scan reads every member the scrub redacts and no other.
var secretMemberValue = regexp.MustCompile(
	`(?i)\\?"(` + secretMemberNames() + `)\\?"\s*:\s*\\?"([^"\\]*)\\?"`)

// secretMemberNames answers the secret member names as one alternation of the pattern above.
func secretMemberNames() string {
	quoted := make([]string, 0, len(SecretMembers))
	for _, member := range SecretMembers {
		quoted = append(quoted, regexp.QuoteMeta(member))
	}
	return strings.Join(quoted, "|")
}

// secretMemberFindings lists every secret member of one cassette that the scrub left readable.
func secretMemberFindings(path string, raw []byte) []string {
	findings := []string{}
	for _, match := range secretMemberValue.FindAllSubmatch(raw, -1) {
		if string(match[2]) == RedactedValue {
			continue
		}
		findings = append(findings, path+" carries a readable value for the member "+string(match[1]))
	}
	return findings
}

// CassetteFindings lists every credential the cassettes under dir carry. A directory that holds
// no cassette, and a directory that does not exist, both read as clean.
func CassetteFindings(dir string) ([]string, error) {
	findings := []string{}

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != cassetteSuffix {
			return nil
		}
		raw, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the cassette directory.
		if err != nil {
			return err
		}
		if match := scrubbedHeaderLine.Find(raw); match != nil {
			findings = append(findings, path+" carries the header "+string(match))
		}
		if keyAfterTheHeader.Match(raw) {
			findings = append(findings, path+" carries a key next to the name of its header")
		}
		findings = append(findings, secretMemberFindings(path, raw)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

// RequireCleanCassettes fails the test when a cassette under dir carries a credential.
func RequireCleanCassettes(t *testing.T, dir string) {
	t.Helper()
	findings, err := CassetteFindings(dir)
	if err != nil {
		t.Fatalf("the scan of %s failed: %v", dir, err)
	}
	for _, finding := range findings {
		t.Errorf("%s. Record the cassette again and check the scrub.", finding)
	}
}
