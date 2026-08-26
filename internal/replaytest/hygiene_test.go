package replaytest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// cleanCassette is the shape the recorder writes once the scrub has run.
const cleanCassette = `---
version: 2
interactions:
    - id: 0
      request:
        headers:
            Content-Type:
                - application/json
        url: https://api.mailslurp.com/inboxes/0690943a-5b02-41b5-b80a-d486a256215a
        method: GET
      response:
        body: '{"id":"0690943a-5b02-41b5-b80a-d486a256215a"}'
        headers:
            Content-Type:
                - application/json
        code: 200
`

// plantCassette writes one cassette into a temporary directory.
func plantCassette(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name+cassetteSuffix)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestTheScanAcceptsAScrubbedCassette(t *testing.T) {
	dir := t.TempDir()
	plantCassette(t, dir, "TestClean", cleanCassette)

	findings, err := CassetteFindings(dir)
	require.NoError(t, err)
	require.Empty(t, findings, "a scrubbed cassette must pass")
}

// The scan is the last gate before a cassette lands in the repository, so it must catch a key a
// broken scrub left behind. Each planted cassette carries a fake key.
func TestTheScanRejectsAPlantedCredential(t *testing.T) {
	leaks := map[string]string{
		"the key header":        "        headers:\n            X-Api-Key:\n                - " + fakeKey + "\n",
		"the lower case header": "        headers:\n            x-api-key:\n                - " + fakeKey + "\n",
		"an authorization header": "        headers:\n            Authorization:\n                - Bearer " +
			fakeKey + "\n",
		"a cookie":     "        headers:\n            Cookie:\n                - session=opaque\n",
		"a set cookie": "        headers:\n            Set-Cookie:\n                - session=opaque\n",
		"the key in a URL": "        url: https://api.mailslurp.com/inboxes?x-api-key=" +
			"00000000-0000-4000-8000-000000000000\n",
		"a secret in a request body": `        body: '{"awsSecretKey":"AKIA0000000000000000"}'` + "\n",
		"a password in a response body": `        body: '{"basicAuth":{"username":"reader",` +
			`"password":"opensesame"}}'` + "\n",
		"a token in a nested list":             `        body: '{"webhooks":[{"token":"opaque-value"}]}'` + "\n",
		"a key a double-quoted scalar escaped": `        body: "{\"apiKey\":\"opaque-value\"}"` + "\n",
	}

	for what, leak := range leaks {
		t.Run(what, func(t *testing.T) {
			dir := t.TempDir()
			path := plantCassette(t, dir, "TestPlanted", cleanCassette+leak)

			findings, err := CassetteFindings(dir)
			require.NoError(t, err)
			require.NotEmpty(t, findings, "the scan must reject %s", what)
			require.Contains(t, findings[0], path, "the finding must name the cassette")
		})
	}
}

// The scan is the last gate, and the scrub is the first. A member the scrub redacts and the scan
// reads past would ship a readable credential, so the scan must reject every one of them.
func TestTheScanRejectsEverySecretMemberTheScrubRedacts(t *testing.T) {
	for _, member := range SecretMembers {
		t.Run(member, func(t *testing.T) {
			dir := t.TempDir()
			path := plantCassette(t, dir, "TestPlanted", cleanCassette+
				`        body: '{"outer":[{"`+member+`":"opaque-value"}]}'`+"\n")

			findings, err := CassetteFindings(dir)
			require.NoError(t, err)
			require.NotEmptyf(t, findings, "the scan must reject the member %s", member)
			require.Contains(t, findings[0], path, "the finding must name the cassette")
			require.Contains(t, findings[0], member, "the finding must name the member")
		})
	}
}

// A body the scrub redacted still names its members, so the scan must read the value and accept
// the one the scrub wrote. A scan that flagged the name alone would fail every recorded cassette.
func TestTheScanAcceptsARedactedSecretMember(t *testing.T) {
	dir := t.TempDir()
	plantCassette(t, dir, "TestRedacted", cleanCassette+
		`        body: '{"awsSecretKey":"REDACTED","basicAuth":{"password":"REDACTED"}}'`+"\n")

	findings, err := CassetteFindings(dir)
	require.NoError(t, err)
	require.Empty(t, findings, "a redacted body must pass")
}

// The scan reads cassettes. A seed file next to one holds no credential and no header.
func TestTheScanReadsOnlyTheCassettes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "TestOne.seed"),
		[]byte("x-api-key: "+fakeKey), 0o600))

	findings, err := CassetteFindings(dir)
	require.NoError(t, err)
	require.Empty(t, findings, "the scan reads the cassettes only")
}

// Nobody has recorded yet, so the directory of a suite can be empty or absent.
func TestTheScanPassesOnADirectoryThatHoldsNoCassette(t *testing.T) {
	findings, err := CassetteFindings(t.TempDir())
	require.NoError(t, err)
	require.Empty(t, findings)

	findings, err = CassetteFindings(filepath.Join(t.TempDir(), "nothing"))
	require.NoError(t, err)
	require.Empty(t, findings)
}

// The committed cassettes of this repository must carry no credential. Both directories are
// empty until a person records, and the scan reads them either way.
func TestTheCommittedCassettesCarryNoCredential(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("..", "..", "examples", "testdata", "cassettes"),
		filepath.Join("..", "..", "provider", "internal", "testdata", "cassettes"),
	} {
		RequireCleanCassettes(t, dir)
	}
}
