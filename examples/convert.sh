#!/usr/bin/env bash
# Usage: examples/convert.sh <nodejs|python|dotnet|go> [source directory] [destination]
# Converts examples/base into one language, then restores what the converter cannot know.

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

SDK_PACKAGE_NODEJS=pulumi-mailslurp
SDK_PACKAGE_PYTHON=pulumi_mailslurp
SDK_PACKAGE_DOTNET=Jschady.Mailslurp
SDK_MODULE_GO=github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp

refuse() {
	echo "$1" >&2
	exit 1
}

# The converter names each language differently from the directory it writes here.
converter_language() {
	case "$1" in
	nodejs) echo typescript ;;
	python) echo python ;;
	dotnet) echo csharp ;;
	go) echo go ;;
	*) refuse "convert.sh takes one of nodejs, python, dotnet or go. It read \"$1\"." ;;
	esac
}

# The converter reports an unknown resource or property on the error stream and still exits with
# the code 0, dropping that resource from the program it writes. So the stream decides, not the code.
convert_into() {
	local language="$1" out="$2" diagnostics status=0
	diagnostics="$(cd "$SOURCE" && pulumi convert \
		--language "$language" --out "$out" --generate-only 2>&1 >/dev/null)" || status=$?
	# The converter colours its own diagnostics whatever the colour switch says, so the escape
	# codes come off before the match.
	diagnostics="$(printf '%s' "$diagnostics" | sed -E "s/$(printf '\033')\[[0-9;]*m//g")"
	[ -z "$diagnostics" ] || printf '%s\n' "$diagnostics" >&2
	[ "$status" -eq 0 ] || refuse "The conversion to $language failed with the status $status."
	if printf '%s' "$diagnostics" | grep 'Error:' >/dev/null; then
		refuse "The conversion to $language reported an error, printed above."
	fi
}

# The provider package is not published, so a version-pinned dependency resolves nowhere. The
# program reads the package this repository builds and links instead.
restore_nodejs() {
	local manifest="$1/package.json"
	jq --indent 2 --arg name "$SDK_PACKAGE_NODEJS" 'del(.dependencies[$name])' \
		"$manifest" > "$manifest.next"
	mv "$manifest.next" "$manifest"
}

restore_python() {
	local manifest="$1/requirements.txt"
	grep -v "^$SDK_PACKAGE_PYTHON==" "$manifest" > "$manifest.next"
	mv "$manifest.next" "$manifest"
}

# The floating prerelease version takes whatever the local feed holds, and the added source is
# where this repository puts the package it built.
restore_dotnet() {
	local manifest
	manifest="$(find "$1" -name '*.csproj')"
	[ -n "$manifest" ] || refuse "The conversion to csharp wrote no project file."
	{
		printf '\t<PropertyGroup Condition="%s != %s">\n' "'\$(PULUMI_LOCAL_NUGET)'" "''"
		printf '\t\t<RestoreAdditionalProjectSources>%s</RestoreAdditionalProjectSources>\n' \
			'$(PULUMI_LOCAL_NUGET)'
		printf '\t</PropertyGroup>\n\n'
	} > "$manifest.property"
	sed -E "s|(\"$SDK_PACKAGE_DOTNET\" Version=)\"[^\"]*\"|\1\"*-*\"|" "$manifest" > "$manifest.next"
	awk -v property="$manifest.property" '/<\/Project>/ && !added {
			while ((getline line < property) > 0) print line
			added = 1
		}
		{ print }' "$manifest.next" > "$manifest"
	rm -f "$manifest.next" "$manifest.property"
}

# The converter writes a module that requires no SDK at all, so the manifest is rebuilt here
# against the copy this repository generates.
restore_go() {
	local out="$1" goversion pulumiversion
	# The Go SDK calls the inbox function LookupInbox, because GetInbox is already the getter of
	# the Inbox resource. The converter writes GetInbox. https://github.com/pulumi/pulumi/issues/22707
	sed -e 's/mailslurp\.GetInbox/mailslurp.LookupInbox/g' "$out/main.go" > "$out/main.go.next"
	mv "$out/main.go.next" "$out/main.go"
	goversion="$(go list -m -f '{{.GoVersion}}')"
	pulumiversion="$(go list -m -f '{{.Version}}' github.com/pulumi/pulumi/sdk/v3)"
	rm -f "$out/go.mod" "$out/go.sum"
	# tidy has to read the SDK, and a relative replace resolves against the destination, so the
	# absolute path is what tidy reads. The committed manifest then takes the relative form.
	(cd "$out" && go mod init github.com/jschady/pulumi-mailslurp/examples/go &&
		go mod edit -go="$goversion" \
			-require="$SDK_MODULE_GO@v0.0.0" \
			-require="github.com/pulumi/pulumi/sdk/v3@$pulumiversion" \
			-replace="$SDK_MODULE_GO=$ROOT/sdk/go/mailslurp" &&
		go mod tidy &&
		go mod edit -replace="$SDK_MODULE_GO=../../sdk/go/mailslurp") > /dev/null
}

# mark_provenance puts the source of the program at the top of it, so a reader who opens the
# converted file learns where the change belongs.
mark_provenance() {
	local out="$1" file="$2" comment="$3" marked="$1/$2.marked"
	{
		printf '%s %s\n\n' "$comment" "$provenance"
		cat "$out/$file"
	} > "$marked"
	mv "$marked" "$out/$file"
}

program_file() {
	case "$1" in
	nodejs) echo "index.ts //" ;;
	python) echo "__main__.py #" ;;
	dotnet) echo "Program.cs //" ;;
	go) echo "main.go //" ;;
	esac
}

provenance="Converted from examples/base by make examples. Change the source program, not this one."

[ "$#" -ge 1 ] && [ "$#" -le 3 ] ||
	refuse "convert.sh takes a language and, for a test of the guard, a source and a destination."

language="$1"
converter="$(converter_language "$language")"
# The source and the destination are arguments so that a test can drive the guard below over a
# program it wrote, without touching the directories this repository ships.
SOURCE="${2:-examples/base}"
out="${3:-$ROOT/examples/$language}"

# A stale file of an earlier conversion reads exactly like a fresh one, so the directory starts empty.
rm -rf "$out"
convert_into "$converter" "$out"
"restore_$language" "$out"
# shellcheck disable=SC2046 # the two words are the file and its comment marker, split on purpose.
mark_provenance "$out" $(program_file "$language")
echo "converted $SOURCE into $out"
