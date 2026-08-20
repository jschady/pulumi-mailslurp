package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const internalReferenceScript = "scripts/check-internal-references.sh"

// The checks live in a script so a contributor can run them before this module compiles.
func TestNoTrackedFileNamesThePrivateBuildNotes(t *testing.T) {
	cmd := exec.Command("./" + internalReferenceScript)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s: %s", internalReferenceScript, out)

	// A scan that reached no file reports a clean pass, so read the count it prints.
	assert.Contains(t, string(out), "tracked files",
		"the check does not report how many files it read")
	assert.NotContains(t, string(out), "read 0 tracked files",
		"the check read no file, so its pass proves nothing")
}

// seededSandbox answers a repository holding a copy of the check and one file carrying line.
// The check reads the tracked files, so the sandbox needs an index; it needs no commit.
func seededSandbox(t *testing.T, line string) string {
	t.Helper()
	sandbox := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sandbox, "scripts"), 0o750); err != nil {
		t.Fatalf("make the sandbox: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(repoRoot(t), internalReferenceScript))
	if err != nil {
		t.Fatalf("read %s: %v", internalReferenceScript, err)
	}
	// The destination joins a TempDir this test made with a constant path, so the taint the
	// analyzer follows never leaves the test.
	//nolint:gosec // G703
	if err := os.WriteFile(filepath.Join(sandbox, internalReferenceScript), body, 0o600); err != nil {
		t.Fatalf("copy the check: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandbox, "seeded.go"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write the seeded file: %v", err)
	}
	for _, args := range [][]string{{"init", "-q", "."}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = sandbox
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return sandbox
}

// seededLine joins its parts into one comment. The parts arrive split so that this file never
// carries a name it seeds, and the check above still passes on the tracked tree.
func seededLine(parts ...string) string {
	return "// " + strings.Join(parts, "") + " explains this"
}

// An empty collision list leaves the strip with no expression to apply, and a line it cannot
// read is a line it cannot refuse. The check has to stop rather than accept the seeded name.
func TestTheInternalReferenceCheckRefusesAnEmptyCollisionList(t *testing.T) {
	sandbox := seededSandbox(t, seededLine("AR", "B-99"))
	script := filepath.Join(sandbox, internalReferenceScript)
	body, err := os.ReadFile(script)
	require.NoError(t, err, "read the copied check")

	emptied := regexp.MustCompile(`(?m)^ALLOWED='.*'$`).ReplaceAll(body, []byte("ALLOWED=''"))
	require.NotEqual(t, body, emptied, "the check declares no collision list, so this test read nothing")
	// The destination is the copy seededSandbox just wrote under TempDir, so the taint the
	// analyzer follows never leaves the test.
	//nolint:gosec // G703
	require.NoError(t, os.WriteFile(script, emptied, 0o600), "write the emptied check")

	//nolint:gosec // G204: the path is this repository's own check script, copied into a
	// directory this test just made under TempDir.
	cmd := exec.Command("bash", script)
	cmd.Dir = sandbox
	out, err := cmd.CombinedOutput()
	require.Errorf(t, err, "the check accepted a seeded reference while its collision list was empty:\n%s", out)
}

// A check that refuses nothing passes every tree. Each line below is seeded into a sandbox and
// the answer is read, so widening the allowlist or dropping a pattern fails here.
func TestTheInternalReferenceCheckAnswersASeededLine(t *testing.T) {
	for _, seeded := range []struct {
		name    string
		line    string
		refused bool
	}{
		{"a ruling id", seededLine("AR", "B-99"), true},
		{"a document citation", seededLine("SP", "EC 1.2"), true},
		{"a bare section sign", seededLine("\u00a7", "4"), true},
		{"a chunk id", seededLine("B", "8-ruleset"), true},
		{"a gate id", seededLine("G", "7"), true},
		{"a spike id", seededLine("AS", "1"), true},
		{"a private document", seededLine("rules", ".md"), true},
		{"the allowed record type", seededLine("the ", "\"A", "6\" record"), false},
		{"an ordinary comment", seededLine("the resource"), false},
	} {
		t.Run(seeded.name, func(t *testing.T) {
			sandbox := seededSandbox(t, seeded.line)
			//nolint:gosec // G204: the path is this repository's own check script, copied into a
			// directory this test just made under TempDir.
			cmd := exec.Command("bash", filepath.Join(sandbox, internalReferenceScript))
			cmd.Dir = sandbox
			out, err := cmd.CombinedOutput()
			if seeded.refused && err == nil {
				t.Errorf("the check accepted %q:\n%s", seeded.line, out)
			}
			if !seeded.refused && err != nil {
				t.Errorf("the check refused %q, which names no private document:\n%s", seeded.line, out)
			}
		})
	}
}
