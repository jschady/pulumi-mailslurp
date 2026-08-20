# Pulumi MailSlurp provider

A Pulumi provider for MailSlurp, written in Go against the MailSlurp REST API. This page is for
somebody who builds the provider. To use it in a program, read the
[provider page in the Pulumi registry](https://www.pulumi.com/registry/packages/mailslurp/).

## The layout

| Directory | What it holds |
| --- | --- |
| `provider/` | the provider, the resources, and the API client |
| `provider/cmd/pulumi-resource-mailslurp/` | the plugin binary and the committed schema |
| `docs/` | the registry pages, the logo, and the example sources |
| `api/openapi.json` | the pinned copy of the MailSlurp API specification |
| `scripts/` | the checks that the lint job runs |

## The build

Build the plugin binary:

```bash
make provider
```

Write the schema of the binary to `provider/cmd/pulumi-resource-mailslurp/schema.json`:

```bash
make generate_schema
```

Write the example pages that each resource description carries:

```bash
make docs
```

## The tests

The unit tests need no API key and no network:

```bash
make test_provider
```

The integration tests call the real API. They create objects in the account of the key, and they
delete each one again.

**Warning:** If you run the integration tests, MailSlurp bills the inboxes that they create. Each
creation also counts against the 30-day creation quota of the account.

```bash
export MAILSLURP_API_KEY=your-api-key
make test_integration
```

## The checks

```bash
make lint
make lint_prose
```

The `lint` target reads the Go code. The `lint_prose` target reads the prose of:

- this page
- the contribution page, `CONTRIBUTING.md`
- the workflow page, `.github/workflows/README.md`
- the registry pages
- every description of the committed schema

## The license

This provider uses the Apache License 2.0. Read the `LICENSE` file for the full text.

test