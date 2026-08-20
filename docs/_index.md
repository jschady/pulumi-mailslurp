---
title: MailSlurp
meta_desc: Provides an overview of the MailSlurp provider for Pulumi.
layout: package
---

The MailSlurp provider creates and manages the email objects of a MailSlurp account:

- inboxes
- webhooks
- inbox rulesets
- inbox forwarders
- email templates

The provider also carries two functions. The `getInbox` function reads one inbox, and the
`getDomain` function reads one custom domain.

## Example

The program below creates one inbox and exports the address that MailSlurp assigns to it.

{{< chooser language "typescript,python,csharp,go" >}}

{{% choosable language typescript %}}

```typescript
import * as mailslurp from "pulumi-mailslurp";

const teamInbox = new mailslurp.Inbox("team-inbox", {
    name: "team-inbox",
    description: "The shared inbox of the team",
    tags: [
        "pulumi",
        "team",
    ],
});

export const emailAddress = teamInbox.emailAddress;
```

{{% /choosable %}}

{{% choosable language python %}}

```python
import pulumi
import pulumi_mailslurp as mailslurp

team_inbox = mailslurp.Inbox("team-inbox",
    name="team-inbox",
    description="The shared inbox of the team",
    tags=[
        "pulumi",
        "team",
    ])

pulumi.export("emailAddress", team_inbox.email_address)
```

{{% /choosable %}}

{{% choosable language csharp %}}

```csharp
using System.Collections.Generic;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() =>
{
    var teamInbox = new Mailslurp.Inbox("team-inbox", new()
    {
        Name = "team-inbox",
        Description = "The shared inbox of the team",
        Tags = new[]
        {
            "pulumi",
            "team",
        },
    });

    return new Dictionary<string, object?>
    {
        ["emailAddress"] = teamInbox.EmailAddress,
    };
});
```

{{% /choosable %}}

{{% choosable language go %}}

```go
package main

import (
	"github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		teamInbox, err := mailslurp.NewInbox(ctx, "team-inbox", &mailslurp.InboxArgs{
			Name:        pulumi.String("team-inbox"),
			Description: pulumi.String("The shared inbox of the team"),
			Tags: pulumi.StringArray{
				pulumi.String("pulumi"),
				pulumi.String("team"),
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("emailAddress", teamInbox.EmailAddress)
		return nil
	})
}
```

{{% /choosable %}}

{{< /chooser >}}

## API key

The provider reads the API key from the `MAILSLURP_API_KEY` environment variable. You can set the
key in the stack configuration instead. The
[installation and configuration page](/registry/packages/mailslurp/installation-configuration/)
carries both commands and the rest of the provider configuration.

## Resource identifier

MailSlurp assigns an identifier to each object that it stores. The provider reports that identifier
as the Pulumi `id` output, and the value matches the identifier the account holds.

## Import of an existing object

You can adopt a MailSlurp object that Pulumi did not create. The provider reads the object, and
`pulumi import` writes the properties of the answer into a generated declaration.

**Warning:** `pulumi import` protects the resource, and `pulumi destroy` refuses a protected
resource. Set `--protect=false` when you plan to destroy the stack later.

Set the API key in the environment, then import the object by its identifier:

```bash
export MAILSLURP_API_KEY=your-api-key
pulumi import mailslurp:index:Inbox adopted 5b9b666c-2451-44a5-94ad-481205ad0ced --protect=false --out adopted.yaml
```

**Warning:** The declaration of a webhook carries the `includeHeaders` values in plain text. If a
header value must stay private, keep it out of the program you commit.

Copy the declaration from `adopted.yaml` into your program, then run `pulumi preview`. The plan
reports no change.

A stack that `pulumi import` creates carries no provider configuration. The provider reads the
environment variable, so `pulumi refresh` and `pulumi destroy` work on such a stack.

The generated declaration carries every property that the API reports. It leaves out a property
that holds no value, and it leaves out the properties below. Write those properties yourself when
your program needs them.

| Resource | The properties that the import cannot answer |
| --- | --- |
| `Inbox` | `domainName`, `domainId`, `useDomainPool`, `useShortAddress`, `prefix`, `expiresIn` |
| `Webhook` | `tags` |

## Cost of an inbox

**Warning:** If you change a property that forces a replacement, Pulumi creates a new inbox. Each
creation counts against the 30-day creation quota of the account, and MailSlurp bills it on a paid
plan. A delete refunds nothing.

## Limitations

Each limitation below is a fact about the MailSlurp API or about a generated SDK. The provider
states it instead of working around it.

### Inbox

The API answers 200 for an update that clears the `name` or the `description` of an inbox, and it
keeps the old value. The provider refuses that change and asks you to replace the inbox. Whether
the empty string clears such a property, where the null value does not, is unconfirmed.

MailSlurp decides `virtualInbox` from the plan of the account. An inbox that you create with the
default options can read `true`, and the provider reports what the API sends.

### Webhook

MailSlurp answers 409 for an update of a webhook that names no inbox and keeps its `url` and its
`eventName`. The API reads the webhook itself as the duplicate. Change the `url` or the
`eventName`, or run `pulumi up --replace <urn>` to replace the webhook.

The API moves a webhook to the account when an update omits the inbox. The provider names the inbox
on every update that sends the whole webhook, so this cannot happen through Pulumi.

This provider does not carry the `aiTransformId` property of the API. An update that sends the whole
webhook drops a transform that you set outside Pulumi. An update of the headers alone keeps the
transform. Whether an update keeps a `basicAuth` value that you set outside Pulumi is unconfirmed.

### Inbox forwarder

MailSlurp gates the inbox forwarder on the plan of the account. The plan of the account that tested
this provider does not enable the inbox forwarder, so the live create is unverified. The tests
cover the create body against recorded answers, and they run against the API with an entitled key.

The API requires the `field` and the `match` properties, or the `matchOptions` property, and the
specification marks neither as required. MailSlurp documents no meaning for the `MATCH` value of
the `should` property, so this page states none. This provider does not carry the
`attachmentTextExtractionMethod` property of the API.

### Custom domain

The account that tested this provider holds no custom domain, so the `getDomain` function is
unverified against live domain data. The tests cover the mapping with recorded answers.

A custom domain becomes verified after your DNS records propagate. MailSlurp does not document how
long the wait is, so `isVerified` can read `false` for some time after you add the records.

### Values that the API sends

The `domainType` output carries the value that MailSlurp sends. The provider does not check the
value against the published list, so a new value reaches your stack unchanged. The `scope` and the
`action` of an inbox ruleset work the same way.

### Python enum members

The Python SDK spells two enum members `AND_` and `OR_`, because `and` and `or` are Python
keywords. Every other member spells the value the API sends.

### Preview

At preview of a create, every output that MailSlurp assigns reads as unknown. Two kinds of output
do not. An optional output that carries no value is absent from the plan. An output that mirrors an
input carries what you wrote, and the `emailAddress` output of an inbox mirrors the address you set.

### Account

MailSlurp publishes no error response for any operation, so the provider models the error body from
live answers. Whether a 429 answer carries `retryAfterSeconds` is unconfirmed. The documentation
names a limit of 150 requests each second. It does not say whether that limit counts one API key or
the whole account.

MailSlurp publishes no quota endpoint and no inbox creation limit for a plan. The provider cannot
check a create against the cap of your account. MailSlurp publishes the limits of the free plan
and not what it allows, so a free account can refuse a resource that this provider creates.

MailSlurp marks an account `FROZEN` when it stops the account. What a write against such an account
answers is unconfirmed, so the provider reports the answer of the API as it arrives.
