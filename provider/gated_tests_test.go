package provider

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	commandsWorkflow   = "pull-request-commands.yml"
	acceptanceWorkflow = "acceptance-tests.yml"
	acceptanceCommand  = "/run-acceptance-tests"
	liveTestsLabel     = "run-live-tests"
	jobCondition       = "\n    if:"
	exampleJob         = "test_examples"
	integrationJob     = "integration"
)

// The jobs that read the API key, plus the sentinel that only waits for the others.
func gatedJobs() map[string]bool {
	return map[string]bool{exampleJob: true, integrationJob: true, "sentinel": true}
}

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	return readRepoFile(t, filepath.Join(".github", "workflows", name))
}

// between answers the text a pair of markers encloses, so a test can read a literal out of the
// workflow that acts on it rather than out of a copy somebody typed twice.
func between(t *testing.T, body, opening, closing, what string) string {
	t.Helper()
	start := strings.Index(body, opening)
	require.GreaterOrEqualf(t, start, 0, "no %s: nothing carries %q", what, opening)
	rest := body[start+len(opening):]
	end := strings.Index(rest, closing)
	require.GreaterOrEqualf(t, end, 0, "no %s: %q is never closed by %q", what, opening, closing)
	return rest[:end]
}

// The note workflow runs on the base branch with this repository's token and its API key. A
// checkout there would run a contributor's code with both, so it checks nothing out.
func TestTheNoteWorkflowRunsNoCodeFromTheBranch(t *testing.T) {
	body := readWorkflow(t, commandsWorkflow)
	require.Contains(t, body, "pull_request_target:",
		"the note workflow must run on pull_request_target to comment on a fork pull request")
	assert.NotContains(t, body, "uses:",
		"the note workflow runs an action; it must run no step that reads the branch")
	assert.Contains(t, body, "pull-requests: write",
		"the note workflow cannot comment without write access to the pull request")
}

// Anyone can comment on a pull request, so the command flow reads who commented before it reads
// anything else. These three associations are the ones GitHub reports for write access.
func TestTheAcceptanceCommandChecksWhoCommented(t *testing.T) {
	condition := between(t, readWorkflow(t, acceptanceWorkflow), "    if: >-", "\n    permissions:",
		"authorize condition")

	for _, required := range []string{
		"github.event.issue.pull_request",
		"github.event.comment.author_association",
		`"OWNER"`,
		`"MEMBER"`,
		`"COLLABORATOR"`,
		acceptanceCommand,
	} {
		assert.Containsf(t, condition, required,
			"the authorize condition never reads %s:\n%s", required, condition)
	}
	assert.NotContains(t, condition, "CONTRIBUTOR\"",
		"CONTRIBUTOR is any account that landed a commit here, not a maintainer")
}

// The maintainer reads the changes, then types the command. A checkout by branch name would run
// whatever landed after that, so the run reads the commit the authorize job recorded.
func TestTheAcceptanceRunReadsOnlyThePinnedCommit(t *testing.T) {
	pin := workflowJobBody(t, acceptanceWorkflow, "authorize")
	assert.Contains(t, pin, `gh api "repos/$REPOSITORY/pulls/$PR_NUMBER" --jq .head.sha`,
		"the authorize job does not read the head commit of the pull request")
	assert.Contains(t, pin, "head_sha=$head_sha",
		"the authorize job never hands the commit on")
	// An empty answer written to the output leaves the checkout below with no ref, and a
	// checkout with no ref reads the default branch.
	assert.Contains(t, pin, `if [ -z "$head_sha" ]; then`,
		"the authorize job hands on a commit it never checked for emptiness")

	run := workflowJobBody(t, acceptanceWorkflow, "acceptance_tests")
	// This link is the whole gate: the condition that reads the commenter sits on the authorize
	// job, so an acceptance job that does not wait for it runs for every comment.
	assert.Contains(t, run, "needs: authorize",
		"the acceptance job does not wait for the authorize job, so nothing checks who asked")
	assert.Contains(t, run, "ref: ${{ needs.authorize.outputs.head_sha }}",
		"the acceptance job does not check out the commit the authorize job pinned")
	assert.NotContains(t, run, "github.head_ref",
		"the acceptance job reads a branch name rather than a commit")
}

// A maintainer asked for this run, so the key is meant to be there. A suite that skipped itself
// for a missing key would report a pass nobody earned.
func TestTheAcceptanceRunRefusesAMissingAPIKey(t *testing.T) {
	run := workflowJobBody(t, acceptanceWorkflow, "acceptance_tests")
	require.Contains(t, run, credentialSecret+": ${{ secrets."+credentialSecret+" }}",
		"the acceptance job never reads %s", credentialSecret)

	refusal := between(t, run, "- name: Refuse a missing API key", "- name: Check out", "refusal step")
	assert.Containsf(t, refusal, "exit 1",
		"the acceptance job reports a missing key without failing:\n%s", refusal)
	assert.Contains(t, run, "scripts/check-tests-ran.sh",
		"the acceptance job must read the run back; a suite that skipped exits green")
}

// The command flow checks a contributor's commit out and runs it. A token that could write to
// this repository would travel with that code.
func TestTheCommandFlowGrantsNoWriteAccess(t *testing.T) {
	body := readWorkflow(t, acceptanceWorkflow)

	granted := between(t, body, "\npermissions:", "\nenv:", "workflow permissions")
	assert.Containsf(t, granted, "contents: read",
		"the command flow does not hold the repository at read:%s", granted)
	assert.NotContainsf(t, granted, "write",
		"the command flow grants write access to the code it runs:%s", granted)
	assert.NotContains(t, workflowJobBody(t, acceptanceWorkflow, "acceptance_tests"), "permissions:",
		"the job that runs the contributor's code widens its own permissions")
}

// The credentialed jobs spend the vendor account, so a pull request reaches them only from this
// repository and only when a maintainer adds the label.
func TestTheLiveJobsNeedTheLabelOnAPullRequest(t *testing.T) {
	body := readWorkflow(t, pullRequestWorkflow)
	assert.Contains(t, between(t, body, "on:", "jobs:", "trigger block"), "- labeled",
		"pull-request.yml does not run on a label, so adding one starts nothing")

	for _, job := range []string{exampleJob, integrationJob} {
		t.Run(job, func(t *testing.T) {
			condition := between(t, workflowJobBody(t, pullRequestWorkflow, job), "if: >-", "steps:",
				"credential gate")
			for _, required := range []string{
				"github.event.pull_request.head.repo.full_name == github.repository",
				"github.event.pull_request.labels.*.name",
				liveTestsLabel,
			} {
				assert.Containsf(t, condition, required,
					"the %s gate never reads %s:\n%s", job, required, condition)
			}
		})
	}
}

// Every check that spends nothing runs on every pull request, fork or not. A condition on one of
// them would report a skip, and a skipped required job reads as a pass.
func TestTheKeylessJobsRunOnEveryPullRequest(t *testing.T) {
	for _, workflow := range buildWorkflows() {
		t.Run(workflow, func(t *testing.T) {
			read := 0
			for _, name := range workflowJobs(t, workflow) {
				if gatedJobs()[name] {
					continue
				}
				read++
				assert.NotContainsf(t, workflowJobBody(t, workflow, name), jobCondition,
					"job %s carries a condition, so it can skip on a pull request", name)
			}
			require.NotZero(t, read, "this workflow declares no keyless job, so the scan read nothing")
		})
	}
}

// The note is the only place a maintainer reads the command from. A rename in the workflow that
// never reached the note would print an instruction that starts nothing.
func TestTheNoteNamesTheCommandAndTheLabelTheWorkflowsRead(t *testing.T) {
	command := between(t, readWorkflow(t, acceptanceWorkflow),
		"startsWith(github.event.comment.body, '", "'", "command the acceptance workflow matches")
	assert.Equal(t, acceptanceCommand, command,
		"the acceptance workflow answers a command this repository never decided on")

	label := between(t, readWorkflow(t, pullRequestWorkflow),
		"contains(github.event.pull_request.labels.*.name, '", "'", "label the credential gate reads")
	assert.Equal(t, liveTestsLabel, label,
		"the credential gate reads a label this repository never decided on")

	note := readWorkflow(t, commandsWorkflow)
	for _, named := range []string{"`" + command + "`", "`" + label + "`"} {
		assert.Containsf(t, note, named, "the note never names %s, so a maintainer cannot find it", named)
	}
}
