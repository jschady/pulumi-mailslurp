#!/usr/bin/env bash

set -uo pipefail

usage() {
	cat <<EOF
NAME
  check-tests-ran.sh - Confirms every listed test started and that none of them skipped itself.

SYNOPSIS
  $0 <test-list-file> <go-test-log> [allowed-skips-file]

DESCRIPTION
  The list holds one test name per line. The log is what \`go test -v\` wrote. A test that
  never started, and a test that skipped itself, both fail this check.

  The job that runs these tests holds the credentials, so a skip there is a misconfiguration
  and not a pass. The allowed-skips file names the exceptions: one test per line, # comments
  carrying the reason, for the tests the account's own shape keeps from running. An allowed
  test must still start, and an allowed name the list does not carry fails as dead wiring.
EOF
}

if [[ "${1:-}" == "help" || "${1:-}" == "-h" || "${1:-}" == "--help" || "$#" -lt 2 || "$#" -gt 3 ]]; then
	usage
	exit 1
fi

list=$1
log=$2
allowed=${3:-}

if [[ ! -s "${list}" ]]; then
	echo "${list} names no test, so this check would pass without reading a log."
	exit 1
fi
if [[ ! -f "${log}" ]]; then
	echo "${log} does not exist, so the run wrote nothing to read."
	exit 1
fi
if [[ -n "${allowed}" && ! -f "${allowed}" ]]; then
	echo "${allowed} does not exist, so this run cannot know which skips are allowed."
	exit 1
fi

# A name the list no longer carries is dead wiring: the test was renamed or removed, and the
# stale allowance would silently cover a future test that takes the name.
if [[ -n "${allowed}" ]]; then
	while read -r name; do
		[[ -z "${name}" || "${name}" == \#* ]] && continue
		if ! grep -qxF "${name}" "${list}"; then
			echo "${allowed} allows ${name}, but ${list} does not carry that test. Remove the stale entry."
			exit 1
		fi
	done <"${allowed}"
fi

skip_is_allowed() { # skip_is_allowed <test-name>
	[[ -n "${allowed}" ]] && grep -qxF "$1" "${allowed}"
}

while read -r name; do
	[[ -z "${name}" ]] && continue
	if ! grep -q "^=== RUN   ${name}\$" "${log}"; then
		echo "${name} never started. The job read a green exit code from a run that skipped it."
		exit 1
	fi
	if grep -qE "^--- SKIP: ${name}( |\$)" "${log}"; then
		if skip_is_allowed "${name}"; then
			echo "${name} skipped itself, and ${allowed} allows it"
			continue
		fi
		echo "${name} skipped itself. This job only runs when the credentials are present, so a skip is a misconfiguration."
		exit 1
	fi
	echo "${name} ran"
done <"${list}"
