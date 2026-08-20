# The workflows

This directory holds the 6 workflows of this repository. The table below names what each one does.
Every job runs on `ubuntu-latest`, and no job calls a Pulumi-organization service.

| File | Trigger | Jobs |
| --- | --- | --- |
| `pull-request.yml` | a pull request, a new label on one, or a manual run | `prerequisites`, `build_sdks`, `compile_examples`, `examples`, `test_examples`, `lint`, `unit`, `integration`, `sentinel` |
| `main.yml` | a push to `main` | the same 9 jobs, plus `snapshot` |
| `release.yml` | a push of a `v*.*.*` tag | `prerequisites`, `build_sdks`, `unit`, `confirm_sdks`, `publish_provider`, `publish_sdks`, `publish_go_sdk` |
| `spec-drift.yml` | 06:17 UTC each Monday, or a manual run | `spec_diff` |
| `pull-request-commands.yml` | a new pull request | `note` |
| `acceptance-tests.yml` | a comment on a pull request | `authorize`, `acceptance_tests` |

The `sentinel` job needs every other job in the workflow. It passes when each one reports success or a
skip, and it fails on any other result. Branch protection needs one required check, so require
`sentinel`.

## The job graph

The `prerequisites` job builds the schema and the provider binary, then uploads the binary as an
artifact. The `build_sdks` job builds one SDK for each of the 4 languages and uploads each one. It
reads the committed schema, so it opens no binary. The `test_examples` job downloads the provider
binary. The other jobs need no artifact.

The `release.yml` workflow publishes in a fixed order: the provider binaries first, then the npm,
PyPI, and NuGet packages, then the Go SDK tag. Each publish job needs the one before it, so a failure
stops the rest. A push of a `v*.*.*` tag starts `release.yml` alone. The `tags-ignore` list in
`main.yml` also holds `sdk/**`, so the Go SDK tag starts no build.

## The gate before the binaries publish

The `confirm_sdks` job restores all 4 SDK artifacts and runs `./scripts/check-sdk-artifacts.sh` over
them. That script reads the name and the version each staged artifact would publish under. It reads
4 of them:

- the npm manifest
- the Python wheel and its source distribution
- the NuGet package
- the Go module path

The `publish_provider` job needs this one, so a release page never carries binaries that no SDK can
be published against. A path that exists on its own proves nothing, so the names are read instead.

## The 3 jobs that read the examples

The `compile_examples` job runs `make compile_examples`, which builds all 4 example programs against
the SDKs this build generates. It ran a test filter that matched no test before, which builds the
test package and opens none of the programs.

The `examples` job reads no API key. It runs on every pull request, forks included. The job converts
the source program into the 4 languages with `make examples`, then fails when `git status` reports a
difference. It then runs the example tests that carry no build tag.

The `test_examples` job runs `make test_examples`, which is one process carrying every build tag. One
process for each language paid for a shared inbox each, and the account bills every inbox.

## The lint job

The `lint` job runs 5 checks, in this order.

1. `./.github/workflows/check-workflows.sh` asserts the rules of this directory.
2. That script runs `actionlint` over every workflow file.
3. `./scripts/check-internal-references.sh` reads every tracked file for a reference to a
   private document.
4. `make lint` reads the Go code.
5. `make lint_prose` reads the prose of this repository, down to every description of the
   committed schema.

The job installs `actionlint` from its GitHub release page and checks the archive against the
`ACTIONLINT_SHA256` digest that the workflow pins. When `actionlint` is absent and `CI` is `true`,
the check script fails instead of skipping. A local run without `actionlint` skips that one
assertion and says so.

## The pinned versions

Every `uses:` line names an exact release tag, and every tool install names an exact version. The
workflows install the Pulumi CLI from the GitHub release page of `pulumi/pulumi`, and they install
`pulumictl` and `golangci-lint` with `go install`. The `ACTIONLINT_VERSION` and `ACTIONLINT_SHA256`
variables pin `actionlint`, and `check-workflows.sh` holds the same 2 values.

## The secrets

| Secret | Workflow | Use |
| --- | --- | --- |
| `MAILSLURP_API_KEY` | `pull-request.yml`, `main.yml`, `acceptance-tests.yml` | the API key for `test_examples` and `integration` |
| `NPM_TOKEN` | `release.yml` | the value that npm publishes with |
| `NUGET_PUBLISH_KEY` | `release.yml` | the NuGet push key |
| `GITHUB_TOKEN` | every workflow | supplied by GitHub |

The PyPI publish step needs no secret. PyPI trusts this repository through OpenID Connect, so the job
asks for the `id-token: write` permission. A human must register the trusted publisher on PyPI once.

## The gate on the API key

2 jobs spend money: `test_examples` and `integration`. On a pull request both need 2 things: the
branch comes from this repository, and the pull request carries the `run-live-tests` label.

A pull request without the label skips both jobs. A pull request from a fork skips both jobs. Every
pull request still gets these checks:

- the provider build
- the lint
- the unit tests
- the example compile
- the example conversion and the example tests that read no API key

## The acceptance-test command

A fork pull request never gets the API key from `pull-request.yml`. A maintainer starts the
acceptance tests with a comment instead.

1. The `pull-request-commands.yml` workflow posts the command list on a new pull request. It checks
   nothing out, so no code from the branch runs there.
2. A maintainer reads the changes.
3. The maintainer comments `/run-acceptance-tests` on the pull request.
4. The `authorize` job reads `author_association`. Only `OWNER`, `MEMBER`, and `COLLABORATOR` pass.
   The job then reads the head commit of the pull request and reports it in a comment.
5. The `acceptance_tests` job checks out that commit by its SHA. It runs `make test_integration` and
   `make test_examples`.

**Warning:** The command runs the code of the pull request with the API key of this repository. Read
the changes before you comment.

The `acceptance_tests` job fails when `MAILSLURP_API_KEY` is empty. A maintainer asked for the run,
so a suite that skipped itself for a missing key would report a pass nobody earned.

## The integration gate

The `test_integration` target exits 0 when no file carries the `integration` build tag. An empty run
with a green exit code is a false pass, so the `integration` job runs 3 steps:

1. List the tests that the `integration` tag adds. The step fails when that list is empty.
2. Run `make test_integration` with `GOTEST='go test -v'`.
3. Run `./scripts/check-tests-ran.sh` over the list and the log. That script fails when a listed
   test never printed `=== RUN`, and when a listed test skipped itself.

There is one copy of that script. Every job that runs the credentialed tests calls it, so a fix to
the comparison reaches each one.

## The coverage profile

The `unit` job runs `make test_provider` with
`GOTEST='go test -coverprofile=coverage.txt -covermode=atomic'`, then uploads `coverage.txt` as an
artifact. The `.gitignore` file and the `clean` target already name that file. No external service
reads the profile.

## The spec drift check

The `spec_diff` job fetches the live MailSlurp spec from `https://api.mailslurp.com/v2/api-docs`. It
compares the set of `paths` keys with the set in `api/openapi.json`, and it fails when a path appears
on one side only. The job never compares the version string in the spec. MailSlurp raises that string
for changes that leave the surface alone, so a version compare reports drift every week.

## The worktree checks

The `prerequisites` job runs `git status --porcelain` after schema generation. The `build_sdks` job
runs it after each SDK build, and the `examples` job runs it after the conversion. A dirty worktree
means one of these is stale:

- the committed schema
- a committed SDK
- a converted example program

Run that generation target again and commit the result.

## Why the 3 build workflows repeat each other

Each workflow holds its own copy of the setup steps. A shared composite action or a called workflow can
remove the repetition. Neither is in place now.
