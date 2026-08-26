#!/usr/bin/env bash
# Pins a MailSlurp API document.
#
# Usage: scripts/pin-spec.sh <spec.json>
#
# The script copies the document over api/openapi.json, then rewrites the 2 constants in
# provider/foundation_test.go that describe the pinned document: the path count and the version
# string. The spec-drift workflow runs it when the live spec moved, and you can run it by hand.

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

count="$(jq '.paths | length' api/openapi.json)"
version="$(jq -r '.info.version' api/openapi.json)"
constants=provider/foundation_test.go

sed -i.bak -E \
	-e "s/^(\topenAPIPathCount = )[0-9]+$/\1${count}/" \
	-e "s/^(\tspecInfoVersion  = )\"[^\"]*\"$/\1\"${version}\"/" \
	"$constants"
rm -f "$constants.bak"

# A pattern that matched nothing leaves the old values in place and the unit test failing.
grep -q "^	openAPIPathCount = ${count}$" "$constants" || {
	echo "The path count in $constants did not change. Set openAPIPathCount to ${count} by hand." >&2
	exit 1
}
grep -q "^	specInfoVersion  = \"${version}\"$" "$constants" || {
	echo "The version in $constants did not change. Set specInfoVersion to ${version} by hand." >&2
	exit 1
}

echo "Pinned the spec with ${count} paths at version ${version}."
