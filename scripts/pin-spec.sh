#!/usr/bin/env bash
# Pins a MailSlurp API document.
#
# Usage: scripts/pin-spec.sh <spec.json>
#
# The script copies the document over api/openapi.json and prints what it pinned. The spec-drift
# workflow runs it when the live spec moved, and you can run it by hand.

set -euo pipefail

cd "$(dirname "$0")/.."

spec="${1:-}"
if [ -z "$spec" ]; then
	echo "Usage: $0 <spec.json>" >&2
	exit 2
fi
if ! jq -e '.openapi and .paths' "$spec" >/dev/null 2>&1; then
	echo "The file $spec is not an OpenAPI document with paths." >&2
	exit 1
fi

cp "$spec" api/openapi.json

# Nothing reads these 2 values: the tests read api/openapi.json itself. They tell the reader of
# the run what the new document holds.
count="$(jq '.paths | length' api/openapi.json)"
version="$(jq -r '.info.version' api/openapi.json)"

echo "Pinned the spec with ${count} paths at version ${version}."
