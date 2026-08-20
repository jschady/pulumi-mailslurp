#!/usr/bin/env bash

set -uo pipefail

usage() {
	cat <<EOF
NAME
  check-tests-ran.sh - Confirms every listed test started and that none of them skipped itself.

SYNOPSIS
  $0 <test-list-file> <go-test-log>

DESCRIPTION
  The list holds one test name per line. The log is what \`go test -v\` wrote. A test that
  never started, and a test that skipped itself, both fail this check.

  The job that runs these tests holds the credentials, so a skip there is a misconfiguration
  and not a pass. One copy of the comparison, so a fix to it reaches every job that runs it.
EOF
}

if [[ "${1:-}" == "help" || "${1:-}" == "-h" || "${1:-}" == "--help" || "$#" -ne 2 ]]; then
	usage
	exit 1
fi

list=$1
log=$2

if [[ ! -s "${list}" ]]; then
	echo "${list} names no test, so this check would pass without reading a log."
	exit 1
fi
if [[ ! -f "${log}" ]]; then
	echo "${log} does not exist, so the run wrote nothing to read."
	exit 1
fi

while read -r name; do
	[[ -z "${name}" ]] && continue
	if ! grep -q "^=== RUN   ${name}\$" "${log}"; then
		echo "${name} never started. The job read a green exit code from a run that skipped it."
		exit 1
	fi
	if grep -qE "^--- SKIP: ${name}( |\$)" "${log}"; then
		echo "${name} skipped itself. This job only runs when the credentials are present, so a skip is a misconfiguration."
		exit 1
	fi
	echo "${name} ran"
done <"${list}"
