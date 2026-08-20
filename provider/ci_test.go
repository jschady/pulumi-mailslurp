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

const (
	pullRequestWorkflow = "pull-request.yml"
	//nolint:gosec // G101: this names the secret a workflow reads, and holds no key.
	credentialSecret    = "MAILSLURP_API_KEY"
	workflowCheckScript = ".github/workflows/check-workflows.sh"
)

// The two workflows that carry the build graph. A rule that held in one of them and not the other
// would leave the branch a release is cut from unchecked.
func buildWorkflows() []string { return []string{pullRequestWorkflow, snapshotWorkflow} }

// exampleTestRuns answers the go test invocations over the examples package in one job body.
func exampleTestRuns(job string) []string {
	var found []string
	for _, line := range strings.Split(job, "\n") {
		if strings.Contains(line, "go test") && strings.Contains(line, "./examples/") {
			found = append(found, strings.TrimSpace(line))
		}
	}
	return found
}

// The compile job ran the test binary with a filter that matched no test, which builds the test
// package and never opens the example programs. A program that stopped compiling passed.
func TestTheCompileJobCompilesTheExamplePrograms(t *testing.T) {
	for _, workflow := range buildWorkflows() {
		t.Run(workflow, func(t *testing.T) {
			job := workflowJobBody(t, workflow, "compile_examples")
			assert.Contains(t, job, "make compile_examples",
				"the compile job must build the four example programs")
			assert.Empty(t, exampleTestRuns(job),
				"a test run in the compile job compiles the test package rather than the programs")
		})
	}
}

// The example tests that read no credential caught a red suite once and nothing in CI ran them, so
// a fork pull request never sees them. This job needs no key, so it runs everywhere.
func TestOneKeylessJobRunsTheExampleTestsThatNeedNoCredential(t *testing.T) {
	for _, workflow := range buildWorkflows() {
		t.Run(workflow, func(t *testing.T) {
			var runners []string
			for _, name := range workflowJobs(t, workflow) {
				job := workflowJobBody(t, workflow, name)
				for _, run := range exampleTestRuns(job) {
					if !strings.Contains(run, "-tags") {
						runners = append(runners, name)
					}
				}
			}
			require.Len(t, runners, 1,
				"one job should run the untagged example tests, and these do: %v", runners)

			job := workflowJobBody(t, workflow, runners[0])
			assert.NotContains(t, job, credentialSecret,
				"the untagged example tests need no credential, so this job reads none")
			assert.NotContains(t, job, "pull_request.head.repo",
				"a credential gate would skip these tests on the pull requests that need them most")
		})
	}
}

// The four converted programs are generated from one source, and nothing compared them with what
// the source produces today. A source change that nobody regenerated would ship the old programs.
func TestOneJobRegeneratesTheExamplesAndRefusesADifference(t *testing.T) {
	for _, workflow := range buildWorkflows() {
		t.Run(workflow, func(t *testing.T) {
			var regenerators []string
			for _, name := range workflowJobs(t, workflow) {
				if strings.Contains(workflowJobBody(t, workflow, name), "make examples") {
					regenerators = append(regenerators, name)
				}
			}
			require.Len(t, regenerators, 1,
				"one job should regenerate the examples, and these do: %v", regenerators)

			job := workflowJobBody(t, workflow, regenerators[0])
			assert.Contains(t, job, "git status --porcelain",
				"the regeneration proves nothing unless the difference is read")
		})
	}
}

// One process runs every tag. A job for each language created one shared inbox each, which spends
// the whole budget of a run on the setup of four processes.
func TestTheCredentialedExampleJobRunsOneProcessOverEveryTag(t *testing.T) {
	for _, workflow := range buildWorkflows() {
		t.Run(workflow, func(t *testing.T) {
			job := workflowJobBody(t, workflow, "test_examples")
			assert.Contains(t, job, "make test_examples",
				"the credentialed job must run the target that carries every tag")
			assert.NotContains(t, job, "matrix",
				"a matrix runs one process for each language and each pays for the shared inbox")
			assert.Contains(t, job, credentialSecret, "the credentialed job must read the key")

			for _, run := range exampleTestRuns(job) {
				assert.NotContains(t, run, "-tags=",
					"a single tag leaves the tests behind every other tag unrun: %s", run)
			}
		})
	}
}

// The SDK generation reads the committed schema, not the built binary, so a job that downloads the
// binary waits on an artifact it never opens.
func TestTheSDKBuildDownloadsNoProviderBinary(t *testing.T) {
	for _, workflow := range append(buildWorkflows(), releaseWorkflow) {
		t.Run(workflow, func(t *testing.T) {
			job := workflowJobBody(t, workflow, "build_sdks")
			assert.NotContains(t, job, "provider.tar.gz",
				"the SDK build reads the committed schema and opens no binary")
		})
	}
}

// The sentinel is the one required check, so a job it does not wait for can fail unnoticed.
func TestTheSentinelWatchesEveryOtherJob(t *testing.T) {
	for _, workflow := range buildWorkflows() {
		t.Run(workflow, func(t *testing.T) {
			watched := jobNeeds(workflowJobBody(t, workflow, "sentinel"))
			var others []string
			for _, name := range workflowJobs(t, workflow) {
				if name != "sentinel" {
					others = append(others, name)
				}
			}
			assert.ElementsMatch(t, others, watched)
		})
	}
}

// GitHub runs a workflow named .yaml as readily as one named .yml, so a scan that reads one
// spelling leaves the other unchecked. Every caller reads the directory through this.
func workflowFiles(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matched, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", pattern))
		require.NoError(t, err)
		found = append(found, matched...)
	}
	return found
}

// A workflow added later must inherit these rules without anyone remembering to list it, so the
// check reads the directory. It reports what it read, and that report is compared here.
func TestTheWorkflowCheckReadsEveryFileInTheDirectory(t *testing.T) {
	command := exec.Command("./" + workflowCheckScript)
	command.Dir = repoRoot(t)
	answered, err := command.CombinedOutput()
	require.NoError(t, err, "the workflow check must pass:\n%s", answered)

	var reported []string
	for _, line := range strings.Split(string(answered), "\n") {
		if scanned, found := strings.CutPrefix(strings.TrimSpace(line), "scanned"); found {
			reported = strings.Fields(strings.SplitN(scanned, ":", 2)[1])
		}
	}
	require.NotEmpty(t, reported, "the check reports no list of what it read:\n%s", answered)

	var present []string
	for _, path := range workflowFiles(t) {
		present = append(present, filepath.Base(path))
	}
	assert.ElementsMatch(t, present, reported)
}

// shellAssignment answers one variable assignment of the workflow check, so a test can run the
// line the script really carries.
func shellAssignment(t *testing.T, variable string) string {
	t.Helper()
	var found string
	for _, line := range strings.Split(readRepoFile(t, workflowCheckScript), "\n") {
		if strings.HasPrefix(line, variable+"=") {
			found = line
		}
	}
	require.NotEmptyf(t, found, "%s assigns no %s", workflowCheckScript, variable)
	return found
}

// runShell runs one line of the workflow check with the directory it scans supplied as $WF.
func runShell(t *testing.T, line, directory string) string {
	t.Helper()
	//nolint:gosec // G204: the line comes from a file in this repository and the directory is one
	// this test just made under TempDir.
	command := exec.Command("bash", "-c", `WF="$1"; `+line+`; printf '%s' "$WORKFLOWS$MATCHED"`,
		"bash", directory)
	answered, err := command.CombinedOutput()
	require.NoError(t, err, string(answered))
	return string(answered)
}

// The comparison above reads today's directory, and a fixed list spelling today's four files
// satisfies it. The assignment itself is therefore run against a directory this test invents.
func TestTheWorkflowScanReadsWhateverTheDirectoryHolds(t *testing.T) {
	directory := t.TempDir()
	invented := []string{"zz-added-later.yml", "aa-another.yml", "mm-other-spelling.yaml"}
	for _, name := range invented {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("on: push\n"), 0o600))
	}

	assert.ElementsMatch(t, invented,
		strings.Fields(runShell(t, shellAssignment(t, "WORKFLOWS"), directory)),
		"the scan must read the directory it is pointed at, not a list somebody typed")
}

// A release published from a run: line publishes as surely as one the action publishes, and the
// snapshot spells arguments that begin the same way without uploading anything.
func TestThePublishScanReadsAReleaseWrittenEitherWay(t *testing.T) {
	markers := shellAssignment(t, "PUBLISH_MARKERS")
	matches := func(t *testing.T, line string) bool {
		t.Helper()
		directory := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(directory, "probe.yml"), []byte(line+"\n"), 0o600))
		return strings.Contains(runShell(t, markers+`
			MATCHED=no
			grep -qE "$PUBLISH_MARKERS" "$WF/probe.yml" && MATCHED=yes`, directory), "yes")
	}

	for _, publishes := range []string{
		"        run: goreleaser release --clean",
		"          args: release --clean",
		"        run: npm publish --provenance --access public",
		"        run: dotnet nuget push -k \"$NUGET_PUBLISH_KEY\"",
	} {
		assert.Truef(t, matches(t, publishes), "the scan reads %q as no publish step", publishes)
	}

	assert.False(t, matches(t, "          args: release --snapshot --clean --timeout 60m0s"),
		"a snapshot uploads nothing, so the scan must leave it alone")
}

// The table at the top of the workflow page is its index, and a row that lost a job sends a reader
// looking for it into the wrong file. A row that defers to another one is read against that one.
func TestTheWorkflowTableRowNamesEveryJobOfItsWorkflow(t *testing.T) {
	page := readRepoFile(t, filepath.Join(".github", "workflows", "README.md"))

	rows := map[string]string{}
	for _, line := range strings.Split(page, "\n") {
		found := regexp.MustCompile("^\\| `([a-z-]+\\.yml)` \\|(.*)$").FindStringSubmatch(line)
		if found != nil {
			rows[found[1]] = found[2]
		}
	}
	require.NotEmpty(t, rows, "the workflow page carries no table")

	for workflow, row := range rows {
		t.Run(workflow, func(t *testing.T) {
			if strings.Contains(row, "the same") {
				row += rows[pullRequestWorkflow]
			}
			for _, job := range workflowJobs(t, workflow) {
				assert.Containsf(t, row, "`"+job+"`", "the row for %s should name %s", workflow, job)
			}
		})
	}
}

// Every tag the example tests carry runs somewhere. The yaml tag was absent from the matrix, so
// three programs written in Pulumi YAML never ran in CI at all.
func TestTheCredentialedJobCarriesEveryExampleTag(t *testing.T) {
	target, err := runMake(t, "-n", "test_examples")
	require.NoError(t, err, target)
	assert.Contains(t, target, "-tags=all",
		"the target the credentialed job runs must carry every tag the example files declare")
}
