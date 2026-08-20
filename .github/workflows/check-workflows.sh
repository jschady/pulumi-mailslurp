#!/usr/bin/env bash
# Check script for pulumi-mailslurp/.github/workflows.
# Static assertions over the four workflows, plus behaviour tests that execute the
# spec-drift compare block and the integration-ran gate against doctored inputs.

set -uo pipefail

WF="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$WF/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
fail() { echo "FAIL  $*"; fails=$((fails + 1)); }
pass() { echo "pass  $*"; }
check() { # check <description> <exit status>
	if [ "$2" -eq 0 ]; then pass "$1"; else fail "$1"; fi
}

PINS="actions/checkout@v7.0.1
actions/setup-go@v7.0.0
actions/setup-node@v7.0.0
actions/setup-python@v7.0.0
actions/setup-dotnet@v6.0.0
actions/upload-artifact@v7.0.1
actions/download-artifact@v8.0.1
goreleaser/goreleaser-action@v7.2.3
pypa/gh-action-pypi-publish@v1.14.2
pulumi/provider-version-action@v2.0.0
pulumi/publish-go-sdk-action@v1.2.0"

# Read from the directory, in both spellings GitHub runs. A fixed list or a single glob leaves a
# workflow unscanned, and an unscanned workflow can carry an unpinned action or a publish step.
WORKFLOWS="$(cd "$WF" && ls -1 ./*.yml ./*.yaml 2>/dev/null | sed 's|^\./||' | sort | tr '\n' ' ')"

# The scans below read these paths, so a file spelled .yaml meets the same rules as a .yml.
WORKFLOW_PATHS=()
for name in $WORKFLOWS; do WORKFLOW_PATHS+=("$WF/$name"); done

# uncommented drops the lines YAML drops. A commented-out step is not a step, and a check that
# reads the file as text would otherwise accept one.
uncommented() {
	grep -v '^[[:space:]]*#'
}

# The four this repository is built around. Any other file is held to the same rules.
REQUIRED_WORKFLOWS="pull-request.yml main.yml release.yml spec-drift.yml"

# The actionlint release the lint jobs install. Bump both together or CI fails the checksum.
ACTIONLINT_PIN=1.7.12
ACTIONLINT_SHA256_PIN=8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8

lint_job() { # lint_job <file>: print the body of the lint job
	awk '/^  lint:$/ {inside = 1; next} inside && /^  [a-z_]+:$/ {exit} inside' "$1"
}

echo "== 1. files exist =="
echo "scanned $(printf '%s' "$WORKFLOWS" | wc -w | tr -d ' ') workflow files: $WORKFLOWS"
# -s not -f: an empty file passes every grep-for-absence check below.
for f in $REQUIRED_WORKFLOWS README.md; do
	[ -s "$WF/$f" ]
	check "$f exists and carries content" $?
done
for f in $WORKFLOWS; do
	[ -s "$WF/$f" ]
	check "$f carries content" $?
done
# This script was dead wiring once. The lint job must keep calling it.
for f in pull-request.yml main.yml; do
	uncommented <"$WF/$f" 2>/dev/null | grep './.github/workflows/check-workflows.sh' >/dev/null
	check "$f calls check-workflows.sh from the lint job" $?
done
if [ "$fails" -gt 0 ]; then
	echo
	echo "RESULT: $fails failing assertion(s)"
	exit 1
fi

echo
echo "== 2. every uses: line carries a pin from the allowed list =="
used="$(grep -hoE 'uses: *[^ ]+' "${WORKFLOW_PATHS[@]}" | awk '{print $2}' | sort -u)"
[ -n "$used" ]
check "the workflows carry at least one uses: line" $?
for u in $used; do
	if printf '%s\n' "$PINS" | grep -xF "$u" >/dev/null; then
		pass "pinned action $u"
	else
		fail "action $u is not one of the pinned actions"
	fi
done

echo
echo "== 3. no Pulumi-org infrastructure =="
# A grep that reads no line finds no hit, so count the lines before trusting it.
scanned="$(cat "${WORKFLOW_PATHS[@]}" 2>/dev/null | wc -l | tr -d ' ')"
[ "$scanned" -gt 0 ]
check "the banned-reference scan read $scanned lines" $?
for banned in esc-action pulumi-ubuntu-8core api.pulumi-staging.io get.pulumi.com pulumi/scripts \
	ci-mgmt codecov action-slack mise-action git-status-check-action gno''sis; do
	if grep -riq -- "$banned" "${WORKFLOW_PATHS[@]}"; then
		fail "a workflow references $banned"
	else
		pass "no reference to $banned"
	fi
done
all_runners="$(grep -h 'runs-on:' "${WORKFLOW_PATHS[@]}" | wc -l | tr -d ' ')"
ubuntu_runners="$(grep -h 'runs-on: ubuntu-latest' "${WORKFLOW_PATHS[@]}" | wc -l | tr -d ' ')"
if [ "$all_runners" -gt 0 ] && [ "$all_runners" = "$ubuntu_runners" ]; then
	pass "every runs-on is ubuntu-latest ($ubuntu_runners jobs)"
else
	fail "$((all_runners - ubuntu_runners)) of $all_runners runs-on lines are not ubuntu-latest"
fi

echo
echo "== 4. triggers =="
python3 - "$WF/release.yml" <<'PY'
import re, sys
text = open(sys.argv[1]).read()
head = text.split('\njobs:')[0]
ok = re.search(r"tags:\s*\n\s*- ['\"]v\*\.\*\.\*['\"]", head) or re.search(r"tags: \[ ?['\"]v\*\.\*\.\*['\"] ?\]", head)
bad = [w for w in ('pull_request', 'schedule', 'branches:', 'workflow_dispatch') if w in head]
sys.exit(0 if ok and not bad else 1)
PY
check "release.yml triggers only on v*.*.* tags" $?

grep -q "tags-ignore:" "$WF/main.yml" && grep -q "'v\*'" "$WF/main.yml" && grep -q "'sdk/\*\*'" "$WF/main.yml"
check "main.yml carries tags-ignore v* and sdk/**" $?

grep -q "paths-ignore:" "$WF/pull-request.yml" && grep -q "CHANGELOG.md" "$WF/pull-request.yml"
check "pull-request.yml ignores CHANGELOG.md" $?

grep -q "workflow_dispatch" "$WF/pull-request.yml"
check "pull-request.yml has workflow_dispatch" $?

grep -q "cron: '17 6 \* \* 1'" "$WF/spec-drift.yml" && grep -q "workflow_dispatch" "$WF/spec-drift.yml"
check "spec-drift.yml runs weekly and on demand" $?

echo
echo "== 5. no publishing outside release.yml =="
# The snapshot spells its own arguments "release --snapshot --clean", which this pattern leaves
# alone: a snapshot uploads nothing.
PUBLISH_MARKERS='npm publish|dotnet nuget push|gh-action-pypi-publish|publish-go-sdk-action|twine upload|args: release --clean|goreleaser release'
for f in $WORKFLOWS; do
	[ "$f" = release.yml ] && continue
	if grep -qE "$PUBLISH_MARKERS" "$WF/$f"; then
		fail "$f carries a publish step"
	else
		pass "$f carries no publish step"
	fi
done
grep -qE "$PUBLISH_MARKERS" "$WF/release.yml"
check "the publish markers match release.yml, so the scan above is not vacuous" $?
grep -q 'args: release --clean' "$WF/release.yml"
check "release.yml runs goreleaser release" $?
grep -q 'snapshot' "$WF/main.yml"
check "main.yml builds a snapshot" $?

echo
echo "== 5b. the worktree-clean steps do the thing =="
for f in pull-request.yml main.yml release.yml; do
	named="$(grep -c 'Check the worktree is clean' "$WF/$f" | tr -d ' ')"
	[ "$named" -ge 2 ]
	check "$f names $named worktree-clean steps; the schema and each SDK need one" $?
	# A step name proves nothing. Each named step must run the git command in its own body.
	python3 - "$WF/$f" <<'PY'
import re, sys
lines = open(sys.argv[1]).read().splitlines()
named = gutted = 0
for i, line in enumerate(lines):
    m = re.match(r'^(\s*)- name: Check the worktree is clean\s*$', line)
    if not m:
        continue
    named += 1
    indent = len(m.group(1))
    body = []
    for nxt in lines[i + 1:]:
        if nxt.strip() and (len(nxt) - len(nxt.lstrip())) <= indent:
            break
        body.append(nxt)
    if 'git status --porcelain' not in '\n'.join(body):
        gutted += 1
sys.exit(1 if named == 0 or gutted else 0)
PY
	check "$f: every worktree-clean step body runs git status" $?
done

echo
echo "== 5c. no pull_request_target workflow checks a branch out =="
# Such a workflow runs with this repository's token and its secrets. Checking the branch out would
# run a contributor's code there. https://gh.io/securely-using-pull_request_target
privileged=0
for f in $WORKFLOWS; do
	grep -q 'pull_request_target' <<<"$(sed -n '/^on:/,/^jobs:/p' "$WF/$f")" || continue
	privileged=$((privileged + 1))
	if grep -qF 'uses: actions/checkout' "$WF/$f"; then
		fail "$f triggers on pull_request_target and checks a branch out"
	else
		pass "$f triggers on pull_request_target and checks nothing out"
	fi
done
[ "$privileged" -gt 0 ]
check "a workflow triggers on pull_request_target, so the scan above read something; the note that tells a maintainer how to run the acceptance tests lives there" $?

echo
echo "== 6. secrets and credential gating =="
secrets="$(grep -hoE 'secrets\.[A-Z_]+' "${WORKFLOW_PATHS[@]}" | sort -u | sed 's/secrets\.//')"
[ -n "$secrets" ]
check "the workflows name at least one secret" $?
for s in $secrets; do
	case "$s" in
	NPM_TOKEN | NUGET_PUBLISH_KEY | MAILSLURP_API_KEY | GITHUB_TOKEN) pass "secret $s is an allowed secret" ;;
	*) fail "secret $s is not an allowed secret" ;;
	esac
done
gate="github.event.pull_request.head.repo.full_name == github.repository"
for job in test_examples integration; do
	python3 - "$WF/pull-request.yml" "$job" "$gate" <<'PY'
import sys
path, job, gate = sys.argv[1], sys.argv[2], sys.argv[3]
lines = open(path).read().splitlines()
start = next((i for i, l in enumerate(lines) if l.startswith(f'  {job}:')), None)
if start is None:
    sys.exit(1)
end = next((i for i in range(start + 1, len(lines)) if lines[i].startswith('  ') and not lines[i].startswith('   ') and lines[i].strip()), len(lines))
sys.exit(0 if gate in '\n'.join(lines[start:end]) else 1)
PY
	check "pull-request.yml job $job is credential gated" $?
done
grep -q 'coverprofile' "$WF/pull-request.yml"
check "pull-request.yml wires a coverage profile" $?

echo
echo "== 7. spec-drift never compares info.version =="
if grep -qE 'info\.version|info"?\]?\.version|infoVersion' "$WF/spec-drift.yml"; then
	fail "spec-drift.yml mentions info.version"
else
	pass "spec-drift.yml never mentions info.version"
fi

extract_run_block() { # extract_run_block <file> <token>
	python3 - "$1" "$2" <<'PY'
import re, sys
path, token = sys.argv[1], sys.argv[2]
lines = open(path).read().splitlines()
for i, line in enumerate(lines):
    m = re.match(r'^(\s*)run: \|\s*$', line)
    if not m:
        continue
    indent = len(m.group(1))
    body = []
    for l in lines[i + 1:]:
        if not l.strip():
            body.append('')
            continue
        if len(l) - len(l.lstrip()) <= indent:
            break
        body.append(l)
    text = '\n'.join(body)
    if token in text:
        base = min(len(l) - len(l.lstrip()) for l in body if l.strip())
        print('\n'.join(l[base:] if l.strip() else '' for l in body))
        sys.exit(0)
sys.exit(1)
PY
}

drift="$TMP/drift.sh"
if extract_run_block "$WF/spec-drift.yml" 'PINNED_SPEC' >"$drift"; then
	pass "spec-drift.yml has a compare block naming PINNED_SPEC"

	jq 'del(.paths["/inboxes"])' "$REPO/api/openapi.json" >"$TMP/removed.json"
	jq '.info.version = "99.0.0"' "$REPO/api/openapi.json" >"$TMP/bumped.json"

	out="$(cd "$REPO" && PINNED_SPEC=api/openapi.json LIVE_SPEC=api/openapi.json bash "$drift" 2>&1)"
	check "compare block exits 0 when the path sets match" $?
	echo "$out" | sed 's/^/      /'

	out="$(cd "$REPO" && PINNED_SPEC=api/openapi.json LIVE_SPEC="$TMP/removed.json" bash "$drift" 2>&1)"
	status=$?
	[ "$status" -ne 0 ]
	check "compare block exits non-zero when a path disappears" $?
	echo "$out" | grep '/inboxes' >/dev/null
	check "compare block names the path that disappeared" $?

	out="$(cd "$REPO" && PINNED_SPEC=api/openapi.json LIVE_SPEC="$TMP/bumped.json" bash "$drift" 2>&1)"
	check "compare block ignores an info.version bump" $?
else
	fail "spec-drift.yml has no compare block naming PINNED_SPEC"
fi

echo
echo "== 8. the integration-ran gate =="
# One copy of the comparison, so a fix to it reaches every job that runs the credentialed tests.
GATE="$REPO/scripts/check-tests-ran.sh"
[ -x "$GATE" ]
check "scripts/check-tests-ran.sh exists and runs" $?
grep -q 'tags=integration -list' "$WF/pull-request.yml"
check "pull-request.yml lists the tests the integration tag adds" $?
for f in pull-request.yml main.yml acceptance-tests.yml; do
	uncommented <"$WF/$f" 2>/dev/null | grep 'scripts/check-tests-ran.sh' >/dev/null
	check "$f runs the gate after the credentialed tests" $?
	# Without the third argument the gate fails the allowed skips too, so the wiring is asserted.
	uncommented <"$WF/$f" 2>/dev/null | grep 'check-tests-ran.sh.*scripts/tests-allowed-to-skip.txt' >/dev/null
	check "$f hands the gate the allowed-skips file" $?
done

if [ -x "$GATE" ]; then
	printf 'TestInboxLifecycleIntegration\n' >"$TMP/integration-tests.txt"

	printf '=== RUN   TestInboxLifecycleIntegration\n--- PASS: TestInboxLifecycleIntegration\n' >"$TMP/integration.log"
	bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" >/dev/null 2>&1
	check "the gate passes when the test started" $?

	printf 'ok  github.com/jschady/pulumi-mailslurp/provider/internal 0.01s\n' >"$TMP/integration.log"
	bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" >/dev/null 2>&1
	[ $? -ne 0 ]
	check "the gate fails when no test started" $?

	# This job only runs with the credentials present, so a self-skip is misconfiguration, not a pass.
	printf '=== RUN   TestInboxLifecycleIntegration\n--- SKIP: TestInboxLifecycleIntegration (0.00s)\nPASS\n' >"$TMP/integration.log"
	! bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" >/dev/null 2>&1
	check "the gate fails when a listed test skipped itself" $?

	printf '=== RUN   TestInboxLifecycleIntegrationExtra\n--- SKIP: TestInboxLifecycleIntegrationExtra (0.00s)\n=== RUN   TestInboxLifecycleIntegration\n--- PASS: TestInboxLifecycleIntegration (1.00s)\n' >"$TMP/integration.log"
	bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" >/dev/null 2>&1
	check "the gate does not read another test's skip as this test's" $?

	# A list nobody wrote would otherwise walk zero names and report a pass.
	: >"$TMP/integration-tests.txt"
	! bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" >/dev/null 2>&1
	check "the gate fails when the list names no test" $?

	# The account holds no verified domain, so scripts/tests-allowed-to-skip.txt names the
	# getDomain legs. The allowance is per name: any other skip still fails, and a stale name
	# is dead wiring the gate refuses.
	printf 'TestInboxLifecycleIntegration\n' >"$TMP/integration-tests.txt"
	printf '=== RUN   TestInboxLifecycleIntegration\n--- SKIP: TestInboxLifecycleIntegration (0.00s)\nPASS\n' >"$TMP/integration.log"
	printf '# the account cannot hold the fixture\nTestInboxLifecycleIntegration\n' >"$TMP/allowed.txt"
	bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" "$TMP/allowed.txt" >/dev/null 2>&1
	check "the gate accepts a skip the allowed file names" $?

	printf 'TestInboxLifecycleIntegration\nTestInboxLifecycleIntegrationExtra\n' >"$TMP/integration-tests.txt"
	printf '=== RUN   TestInboxLifecycleIntegration\n--- PASS: TestInboxLifecycleIntegration (0.10s)\n=== RUN   TestInboxLifecycleIntegrationExtra\n--- SKIP: TestInboxLifecycleIntegrationExtra (0.00s)\n' >"$TMP/integration.log"
	! bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" "$TMP/allowed.txt" >/dev/null 2>&1
	check "the gate still fails a skip outside the allowed names" $?

	printf 'TestInboxLifecycleIntegration\n' >"$TMP/integration-tests.txt"
	printf 'TestInboxLifecycleIntegrationExtra\n' >"$TMP/allowed.txt"
	! bash "$GATE" "$TMP/integration-tests.txt" "$TMP/integration.log" "$TMP/allowed.txt" >/dev/null 2>&1
	check "the gate refuses an allowed name the list does not carry" $?
fi

echo
echo "== 8b. the internal-reference check =="
# The script reads the tracked files for a reference to a document that does not publish.
INTERNAL="$REPO/scripts/check-internal-references.sh"
[ -x "$INTERNAL" ]
check "scripts/check-internal-references.sh exists and runs" $?
for f in pull-request.yml main.yml; do
	uncommented <"$WF/$f" 2>/dev/null | grep './scripts/check-internal-references.sh' >/dev/null
	check "$f calls check-internal-references.sh from the lint job" $?
done

echo
echo "== 9. actionlint =="
for f in pull-request.yml main.yml; do
	body="$(lint_job "$WF/$f" | uncommented)"
	live="$(uncommented <"$WF/$f")"
	read_lines="$(printf '%s\n' "$body" | grep -c . | tr -d ' ')"
	[ "$read_lines" -gt 5 ]
	check "$f: the lint job body read $read_lines lines" $?
	printf '%s\n' "$body" | grep -F 'name: Install actionlint' >/dev/null
	check "$f: the lint job installs actionlint" $?
	printf '%s\n' "$body" | grep -F "actionlint_\${ACTIONLINT_VERSION}_linux_amd64.tar.gz" >/dev/null
	check "$f: the install step downloads the pinned release tarball" $?
	printf '%s\n' "$body" | grep -F 'sha256sum -c -' >/dev/null
	check "$f: the install step verifies the checksum" $?
	# An install step placed after this script runs would leave the check below skipping.
	printf '%s\n' "$body" | awk '/Install actionlint/ {i = NR} /check-workflows.sh/ {c = NR} END {exit !(i && c && i < c)}'
	check "$f: the install step runs before this script" $?
	printf '%s\n' "$live" | grep -F "ACTIONLINT_VERSION: $ACTIONLINT_PIN" >/dev/null
	check "$f: ACTIONLINT_VERSION is pinned to $ACTIONLINT_PIN" $?
	printf '%s\n' "$live" | grep -F "ACTIONLINT_SHA256: $ACTIONLINT_SHA256_PIN" >/dev/null
	check "$f: ACTIONLINT_SHA256 is pinned to the release digest" $?
done

if command -v actionlint >/dev/null 2>&1; then
	(cd "$REPO" && actionlint "${WORKFLOW_PATHS[@]}")
	check "actionlint reports no problem" $?
elif [ "${CI:-}" = "true" ]; then
	fail "actionlint is absent under CI=true; the lint job's install step is what puts it on PATH"
else
	echo "skip  actionlint is not installed"
fi

echo
echo "RESULT: $fails failing assertion(s)"
[ "$fails" -eq 0 ]
