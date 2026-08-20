# How to work on this repository

This repository holds a Pulumi provider that calls the MailSlurp REST API. The provider is native:
it wraps no other provider, and it generates its schema from the Go code.

| Directory | What it holds |
| --- | --- |
| `provider/` | the provider, the resources, and the API client |
| `provider/cmd/pulumi-resource-mailslurp/` | the plugin binary and the committed schema |
| `docs/` | the registry pages, the logo, and the example sources |
| `api/openapi.json` | the pinned copy of the MailSlurp API specification |
| `scripts/` | the checks that the lint job runs |

## The tools

Install these versions. The `.tool-versions` file lists every one of them. The workflows pin most
of the same values in their setup steps. A few come from other sources, so raise a version here and
in the workflows together.

| Tool | Version |
| --- | --- |
| Go | 1.26.4 |
| Pulumi | 3.258.0 |
| golangci-lint | 2.12.2 |
| Node.js | 24.19.0 |
| Python | 3.11 |
| .NET | 8.0 |
| jq | 1.7.1 |

## The first build

1. Clone the repository.
2. Run `make provider` to build the plugin binary into `bin/`.
3. Run `make test_provider` to run the unit tests.

## The generated files

Two sets of files are generated, and a hand edit of either one goes away on the next run.

| File | The target that writes it |
| --- | --- |
| `provider/cmd/pulumi-resource-mailslurp/schema.json` | `make generate_schema` |
| `provider/internal/embed/*.md` | `make docs` |

To change a resource description, edit the `Annotate` method of that resource, then run
`make generate_schema`. To change an example, edit the source in `docs/yaml/`, then run `make docs`
and `make generate_schema`. Commit the generated file with the change that produced it.

## The checks

Run every check with one command:

```bash
make ensure
```

It runs `make tidy`, then `make lint`, then `make test_provider`. These tests need no network.

The prose check runs separately:

```bash
make lint_prose
```

## The live tests

The integration tests call the real API. They create objects in the account of the key, and they
delete each one again.

**Warning:** If you point `MAILSLURP_API_KEY` at an account that holds real mail, a failed test can
leave objects behind. Use an empty account.

```bash
export MAILSLURP_API_KEY=your-api-key
make test_integration
```

An inbox costs money, so the tests share one inbox and count every creation. A run that would
create more than 3 inboxes fails instead.

## The Pulumi home directory

The recipes point `PULUMI_HOME` at `.pulumi` inside this repository, so an installed plugin stays
with the checkout. The `pulumi login` command writes a sign-in file into that directory, and a
test of the build refuses that file. Set `PULUMI_HOME` to a directory outside this repository
before you run `pulumi` by hand:

```bash
export PULUMI_HOME=$(mktemp -d)
```

## The words this repository uses

One concept takes one word, and every part of this repository uses that word. Use the word in this
table and no other.

| Concept | The word |
| --- | --- |
| a member of a schema | property |
| the value that authenticates a call | API key |
| the object that receives email | inbox |
| the thing that Pulumi manages | resource |
| the deployment that holds resources | stack |
| the values that the stack sets on the provider | provider configuration |
| the reader of a page | you |

The provider for Files.com uses the same word for each concept above. A word that drifts here
drifts between two published packages.

The concepts below arrived with this provider, and they take these words.

| Concept | The word |
| --- | --- |
| the value that names one object | identifier |
| the call that MailSlurp makes on an event | webhook |
| the rule that blocks or allows email | inbox ruleset |
| the rule that copies email onward | inbox forwarder |
| the email body with variables | email template |
| the read-only call a program makes | function |
| the DNS name that MailSlurp serves | custom domain |

### The words that never appear

A test reads the list below. No shipped text carries any of these words outside a code sample,
because each one names a concept that already has its word.

- `field`
- `attribute`
- `mailbox`
- `entity`
- `hook`
- `callback`
- `filter`
- `data source`
- `template file`
- `forwarding rule`
- `token`
- `credential`
- `id`
- `we`

## The writing rules

The same rules hold for every user-facing string:

- the registry pages
- the schema descriptions
- the error messages

1. Write one instruction per sentence, and keep a step to 20 words.
2. Keep a statement to 25 words.
3. Write the article and the subject. Never write `Returns email address`.
4. Write every identifier in backticks. Never write one in quotation marks.
5. Write a chain of three items as a list. A chain of identifiers can stay a chain.
6. Put a warning immediately before the step it protects.

The `make lint_prose` target reads these rules where a script can. It reads the pages of this
repository and every description of the committed schema.

## The descriptions in the code

A resource description is a raw string that `dedent` reads. Raw strings cannot hold a backtick, so
write an identifier in double quotation marks there, and `dedent` turns each one into a backtick.
This is the one place where a double quotation mark is right.

## The pull request

1. Write the failing test first.
2. Run it.
3. Read the failure.
4. Write the code that makes it pass.
5. Run `make ensure`.
6. Run `make lint_prose`.
7. Commit the generated files that your change regenerates.
8. Open the pull request.
