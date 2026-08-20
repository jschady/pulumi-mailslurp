# The MailSlurp provider for Pulumi

This provider creates and manages the email objects of a
[MailSlurp](https://www.mailslurp.com) account:

- the inboxes
- the webhooks
- the inbox rulesets
- the inbox forwarders
- the email templates

Two functions, `getInbox` and `getDomain`, read one inbox and one custom domain.

## The installation

Install the package for the language of your program.

| Language | The command |
| --- | --- |
| Node.js | `npm install pulumi-mailslurp` |
| Python | `pip install pulumi_mailslurp` |
| Go | `go get github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp` |
| .NET | `dotnet add package Jschady.Mailslurp` |

## The API key

The provider authenticates every call with the API key of your account:

```bash
export MAILSLURP_API_KEY=your-api-key
```

You can keep the key in the encrypted stack configuration instead. The
[installation and configuration page](./docs/installation-configuration.md) shows that command and
every property the provider reads.

## The documentation

The [MailSlurp page of the Pulumi registry](https://www.pulumi.com/registry/packages/mailslurp/)
serves the API documentation. The [overview page](./docs/_index.md) of this repository holds one
example program per language, and it names the known limitations.

## The contribution

`CONTRIBUTING.md` explains how to build and test this repository. The workflow page,
`.github/workflows/README.md`, explains what each CI job runs.

## The license

This provider uses the Apache License 2.0. Read the `LICENSE` file for the full text.
