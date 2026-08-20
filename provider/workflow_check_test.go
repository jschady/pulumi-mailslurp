package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The probe a seeded test drops into the workflow directory. No allowlist names it, so a check
// that reads the file refuses it and a check that never reads the file stays green.
const (
	probeWorkflow = "probe.yaml"
	probeAction   = "example/probe@v1.0.0"
	probeBody     = `name: probe
on: workflow_dispatch
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
      - name: Run the probe action
        uses: ` + probeAction + `
`
)

// The check runs the drift compare block against this file and runs the no-skip gate, so the
// sandbox carries both. The gate keeps its execute bit: the check reads it as a runnable script.
const (
	driftSpec = "api/openapi.json"
	ranGate   = "scripts/check-tests-ran.sh"
)

// workflowCheckSandbox copies what the workflow check reads into a directory a test can doctor,
// so a seeded violation never reaches the working tree.
func workflowCheckSandbox(t *testing.T) string {
	t.Helper()
	sandbox := t.TempDir()
	source := filepath.Join(repoRoot(t), ".github", "workflows")
	target := filepath.Join(sandbox, ".github", "workflows")
	require.NoError(t, os.MkdirAll(target, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, filepath.Dir(driftSpec)), 0o750))

	entries, err := os.ReadDir(source)
	require.NoError(t, err)
	copied := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		writeSandboxFile(t, filepath.Join(target, entry.Name()),
			readSandboxFile(t, filepath.Join(source, entry.Name())))
		copied++
	}
	require.NotZero(t, copied, "the workflow directory holds no file, so the sandbox proves nothing")

	writeSandboxFile(t, filepath.Join(sandbox, driftSpec),
		readSandboxFile(t, filepath.Join(repoRoot(t), driftSpec)))

	for _, script := range []string{ranGate, internalReferenceScript} {
		copied := filepath.Join(sandbox, script)
		require.NoError(t, os.MkdirAll(filepath.Dir(copied), 0o750))
		writeSandboxFile(t, copied, readSandboxFile(t, filepath.Join(repoRoot(t), script)))
		//nolint:gosec // G302
		require.NoError(t, os.Chmod(copied, 0o700))
	}
	return sandbox
}

// sandboxWorkflow answers the path of one workflow file inside the sandbox.
func sandboxWorkflow(sandbox, name string) string {
	return filepath.Join(sandbox, ".github", "workflows", name)
}

// The two helpers below join a TempDir this test made with a name read from this repository's own
// workflow directory, so the taint the analyzer follows never leaves the test.
func writeSandboxFile(t *testing.T, path, body string) {
	t.Helper()
	//nolint:gosec // G703
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func readSandboxFile(t *testing.T, path string) string {
	t.Helper()
	//nolint:gosec // G304
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

// runWorkflowCheckIn runs the copied check against the sandbox. bash runs it by path, so the copy
// needs no execute bit and every file the sandbox holds stays at 0600.
func runWorkflowCheckIn(t *testing.T, sandbox string) (string, error) {
	t.Helper()
	//nolint:gosec // G204: the path is this repository's own check script, copied into a
	// directory this test just made under TempDir.
	command := exec.Command("bash", sandboxWorkflow(sandbox, "check-workflows.sh"))
	command.Dir = sandbox
	answered, err := command.CombinedOutput()
	return string(answered), err
}

// indentOf answers the count of leading spaces, which is what tells a YAML block from what follows.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// commentOutStep rewrites one step of a workflow into YAML comments. The step disappears from the
// run while every literal a text scan looks for stays on the page.
func commentOutStep(t *testing.T, path, step string) {
	t.Helper()
	lines := strings.Split(readSandboxFile(t, path), "\n")
	start := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "- name: "+step {
			start = index
			break
		}
	}
	require.GreaterOrEqualf(t, start, 0, "%s carries no step named %q", filepath.Base(path), step)

	indent := indentOf(lines[start])
	lines[start] = "#" + lines[start]
	for index := start + 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) != "" && indentOf(lines[index]) <= indent {
			break
		}
		lines[index] = "#" + lines[index]
	}
	writeSandboxFile(t, path, strings.Join(lines, "\n"))
}

// commentOutLine rewrites the one line starting with prefix into a YAML comment.
func commentOutLine(t *testing.T, path, prefix string) {
	t.Helper()
	lines := strings.Split(readSandboxFile(t, path), "\n")
	found := false
	for index, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[index] = "#" + line
			found = true
			break
		}
	}
	require.Truef(t, found, "%s carries no line starting with %q", filepath.Base(path), prefix)
	writeSandboxFile(t, path, strings.Join(lines, "\n"))
}

// TestTheWorkflowCheckPassesInACopyOfTheRepository is the control. Without it every seeded test
// below could pass on a check that refuses something the copy itself broke.
func TestTheWorkflowCheckPassesInACopyOfTheRepository(t *testing.T) {
	answered, err := runWorkflowCheckIn(t, workflowCheckSandbox(t))
	require.NoErrorf(t, err, "the check failed on a copy of the repository:\n%s", answered)
}

// GitHub runs a workflow spelled .yaml as readily as one spelled .yml. A file the check never
// enumerates carries an unreviewed action, secret or publish step into every run.
func TestTheWorkflowCheckReadsAWorkflowSpelledYaml(t *testing.T) {
	sandbox := workflowCheckSandbox(t)
	writeSandboxFile(t, sandboxWorkflow(sandbox, probeWorkflow), probeBody)

	answered, err := runWorkflowCheckIn(t, sandbox)
	require.Errorf(t, err, "the check passed on a .yaml workflow carrying an unreviewed action:\n%s",
		answered)
	assert.Containsf(t, answered, probeAction, "the check never read %s:\n%s", probeWorkflow, answered)
}

// TestTheWorkflowCheckRefusesACommentedOutLintStep covers the class a text scan cannot see: the
// literal stays on the page and the step never runs.
func TestTheWorkflowCheckRefusesACommentedOutLintStep(t *testing.T) {
	for _, step := range []string{"Install actionlint", "Check the workflow rules"} {
		t.Run(step, func(t *testing.T) {
			sandbox := workflowCheckSandbox(t)
			commentOutStep(t, sandboxWorkflow(sandbox, pullRequestWorkflow), step)

			answered, err := runWorkflowCheckIn(t, sandbox)
			require.Errorf(t, err, "the check passed with the %q step commented out:\n%s", step, answered)
		})
	}
}

// A pull_request_target workflow holds this repository's token and its API key, so a checkout of
// the branch would run a contributor's code with both. That is the shape the check must refuse.
func TestTheWorkflowCheckRefusesAPullRequestTargetCheckout(t *testing.T) {
	sandbox := workflowCheckSandbox(t)
	path := sandboxWorkflow(sandbox, commandsWorkflow)
	writeSandboxFile(t, path, readSandboxFile(t, path)+
		"      - name: Check out the branch\n        uses: actions/checkout@v7.0.1\n"+
		"        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n")

	answered, err := runWorkflowCheckIn(t, sandbox)
	require.Errorf(t, err,
		"the check passed on a pull_request_target workflow that checks a branch out:\n%s", answered)
	assert.Containsf(t, answered, "pull_request_target",
		"the check does not name the trigger that makes the checkout unsafe:\n%s", answered)
}

// The scan above finds nothing when no workflow carries the trigger, and finding nothing must not
// read as a pass: the note that tells a maintainer how to run the acceptance tests lives there.
func TestTheWorkflowCheckRefusesAnEmptyPullRequestTargetScan(t *testing.T) {
	sandbox := workflowCheckSandbox(t)
	require.NoError(t, os.Remove(sandboxWorkflow(sandbox, commandsWorkflow)))

	answered, err := runWorkflowCheckIn(t, sandbox)
	require.Errorf(t, err,
		"the check passed with no pull_request_target workflow left to scan:\n%s", answered)
}

// The version and the digest travel together, so a commented pin leaves the install step reading
// an empty variable and the checksum comparing nothing.
func TestTheWorkflowCheckRefusesACommentedOutActionlintPin(t *testing.T) {
	for _, pin := range []string{"  ACTIONLINT_VERSION:", "  ACTIONLINT_SHA256:"} {
		t.Run(strings.TrimSpace(pin), func(t *testing.T) {
			sandbox := workflowCheckSandbox(t)
			commentOutLine(t, sandboxWorkflow(sandbox, pullRequestWorkflow), pin)

			answered, err := runWorkflowCheckIn(t, sandbox)
			require.Errorf(t, err, "the check passed with %s commented out:\n%s",
				strings.TrimSpace(pin), answered)
		})
	}
}
