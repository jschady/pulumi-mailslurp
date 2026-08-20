# Registry pull request

This page is the draft of the pull request that lists this provider on the Pulumi Registry. Nobody
opened it. The registry lists a package only after a release exists, and the release is a human
decision.

The facts on this page match `pulumi/registry` on 20 August 2026. Its default branch is `master`.

## What the pull request changes

The pull request changes two files and nothing else. The registry generates the package metadata
and the documentation after the merge, so the pull request commits no generated file.

### 1. The package entry

`community-packages/package-list.json` holds one object with an `include` array. Append one object
to the end of that array:

```json
{
  "repoSlug": "jschady/pulumi-mailslurp",
  "schemaFile": "provider/cmd/pulumi-resource-mailslurp/schema.json"
}
```

An entry carries these two keys and no other. The array is not sorted, and the merged entries sit
in the order they arrived, so append rather than insert.

### 2. The publisher name

`tools/resourcedocsgen/pkg/publishers/publisher-names.json` maps the `publisher` property of a
schema to the name the registry shows. This schema sets `publisher` to `jschady`, and that key is
absent from the map today. Add one pair, in alphabetical position:

```json
"jschady": "jschady"
```

Publishing fails without the pair. This is the one file the automation accepts beside the package
list, because a new publisher has to arrive with its own entry.

## Checklist before you open it

Each row is a decision a person makes. Work through the table from the top.

| Step | What you do | Why it blocks |
| --- | --- | --- |
| Approve the mark | Look at `docs/logo.svg` and keep it or replace it | The registry page and the NuGet package both show it |
| Decide the code of conduct | Ship without one, or give a contact address | The address belongs to the owner of this repository |
| Re-check the package names | Run `./scripts/check-package-names.sh` | Nothing reserves a name without publishing it |
| Squash the history | Follow the steps under "How to squash the history" | The published history carries one commit |
| Create the live-test label | Add a label named `run-live-tests` to the repository | The label condition in pull-request.yml reads it |
| Decide the manual runs | Keep or remove `workflow_dispatch` on the jobs that read the API key | A maintainer can start a billed run by hand |
| Publish the release | Tag `vX.Y.Z` and let the release workflow run | The automation reads the latest published release |
| Install the plugin | Run the command in the next section | This is the first real test of the download server |

### How to squash the history

The published history carries one commit. Run these steps last, after every other check passes
and before the push. The commit keeps the tree exactly as it is now.

1. Confirm the tree is clean with `git status`.
2. Record the current commit with `git rev-parse HEAD`.
3. Start an orphan branch that keeps the index: `git checkout --orphan release`.
4. Write the one commit with `git commit -m "Initial release"`.
5. Compare the trees with `git diff <recorded-commit> HEAD`. The command prints nothing.
6. Move the branch into place with `git branch -M main`.
7. Tag the new commit with `git tag vX.Y.Z`.
8. Push the branch and the tag.

### What the mark approval has to see

`docs/logo.svg` is original artwork. It draws three things:

- a navy tile
- a white geometric letter M
- one amber circle

The MailSlurp mark is a letter M as well, drawn as green gradient ribbon strokes with no tile.
Nothing is shared between the two drawings:

- no path
- no colour
- no gradient
- no construction
- no container

The subject is shared. Both draw a capital M. The approval is yours to make with that stated
rather than denied.

One reading note. At 64 pixels the amber circle sits close to the terminal of the M and never
touches it. A little more clearance would read better at 32 pixels.

### How to replace the mark

Write the new artwork to `docs/logo.svg` and a 256 by 256 pixel copy to `docs/logo.png`. Then run
`make generate_dotnet`, which copies the second file into the .NET SDK. NuGet reads the leading
bytes of a package icon and rejects an SVG, so the two formats both ship.

### How to check the download server

Run this after the release publishes:

```bash
pulumi plugin install resource mailslurp 0.1.0 --server github://api.github.com/jschady/pulumi-mailslurp
```

## Pull request body

> ### Add the MailSlurp provider
>
> This adds `jschady/pulumi-mailslurp`, a native provider for
> [MailSlurp](https://www.mailslurp.com) email infrastructure. It wraps the MailSlurp REST API
> directly, and it carries 5 resources and 2 functions.
>
> The provider commits a `schema.json` and ships an SDK for each of the 4 languages. This
> repository is Apache 2.0.
>
> `jschady` is a new publisher, so this pull request also adds the display name.

## `/check` command

The automation reads the live provider repository, not the diff in the pull request. To fix a red
check, change this repository and comment `/check` on the pull request. Do not push a new commit to
the registry fork.

1. Read the fact sheet the bot posts on the pull request.
2. Fix what is red here. One of these repairs it:
   - cut a release
   - publish an SDK
   - correct the schema path
3. Comment `/check` on the pull request.
4. Wait for the bot to rewrite its fact sheet.

The command runs at most once every 10 minutes. The author of the pull request can run it, and so
can a maintainer of `pulumi/registry`. A maintainer can also comment `/preview` to build the pages.
A maintainer review is still required to merge, and nothing merges on its own.

## What the automation reads

| Check | Blocking | What it reads |
| --- | --- | --- |
| Changed files | Yes | Only the two files above |
| Published release | Yes | The latest release of `jschady/pulumi-mailslurp` |
| Schema path | Yes | `schemaFile` at that release |
| Documentation build | Yes | `resourcedocsgen metadata from-github` |
| Registry page | Yes | `docs/_index.md` in this repository |
| Plugin install | Yes | `pulumi plugin install resource mailslurp <tag>` |
| SDK install | No | The npm, PyPI and Go packages the schema advertises |
| Documentation lint | No | Relative images and links in `docs/_index.md` |
| Publisher entry | No | The `publisher` property against the map |

The version comes from the release tag. The committed schema carries no version, and the
automation never reads one from it.

Two details of that table are worth knowing before the first release.

- A prerelease does not count. The check asks for a published release, and `.goreleaser.yml` sets
  `prerelease: auto`, which marks a tag such as `v0.1.0-alpha.1` as a prerelease on GitHub.
- The SDK install probe covers npm, PyPI and Go. It runs no probe for NuGet, so a broken .NET
  package reaches the registry unreported.

## Notes

### Page a reader lands on

`docs/_index.md` is the index page of the package, and `docs/installation-configuration.md` is the
Installation and Configuration page. The check reads the first one and refuses without it. Both
pages carry install commands for packages that publish with the first release.

### Python enum members

Two Python enum members keep a trailing underscore: `AND_` and `OR_`. Both words are Python
keywords, so the generator renames them. The other 35 members spell the wire value exactly. A test
in `provider/sdk_generated_test.go` refuses a build that renames any of the 37.

### Dynamic path is a different process

A dynamically bridged Terraform provider, the kind you add with `pulumi package add
terraform-provider <name>`, cannot be listed by pull request. That path needs a "New Package" issue
instead. This provider is native and it commits a schema, so the pull request is correct here.
