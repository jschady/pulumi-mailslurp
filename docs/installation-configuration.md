---
title: MailSlurp Installation & Configuration
meta_desc: Provides an overview on how to configure the Pulumi MailSlurp provider.
layout: package
---

## The packages

The MailSlurp provider ships as one package per language.

| Language | Package |
| --- | --- |
| JavaScript and TypeScript | [`pulumi-mailslurp`](https://www.npmjs.com/package/pulumi-mailslurp) |
| Python | [`pulumi-mailslurp`](https://pypi.org/project/pulumi-mailslurp/) |
| Go | [`github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp`](https://pkg.go.dev/github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp) |
| .NET | [`Jschady.Mailslurp`](https://www.nuget.org/packages/Jschady.Mailslurp) |

## The installation

Run the command for the language that your program uses.

### Node.js

```bash
npm install pulumi-mailslurp
```

### Python

```bash
pip install pulumi-mailslurp
```

### Go

```bash
go get github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp
```

### .NET

```bash
dotnet add package Jschady.Mailslurp
```

## The API key

The provider authenticates with one API key. MailSlurp shows the key in the dashboard of your
account.

**Warning:** If your program source holds the API key, everyone who reads it can read your account.
Keep the key in the environment or in the encrypted stack configuration.

Set the key in the environment:

```bash
export MAILSLURP_API_KEY=your-api-key
```

Or set it in the stack configuration, where Pulumi encrypts it:

```bash
pulumi config set --secret mailslurp:apiKey your-api-key
```

The provider reads the environment variable when the stack configuration carries no `apiKey`.

## The provider configuration

| Property | Environment variable | What it is |
| --- | --- | --- |
| `apiKey` | `MAILSLURP_API_KEY` | The API key that the provider authenticates with. |
| `endpoint` | none | The base URL of the API. The default is `https://api.mailslurp.com`. Set it to reach a mock server. |

## The explicit provider

The program below builds one provider from the stack configuration and creates an inbox with it.
Use this shape when one stack must reach two MailSlurp accounts.

{{< chooser language "typescript,python,csharp,go" >}}

{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const config = new pulumi.Config();

const team = new mailslurp.Provider("team", {
    apiKey: config.requireSecret("teamApiKey"),
});

const support = new mailslurp.Inbox("support", {
    name: "support",
}, { provider: team });
```

{{% /choosable %}}

{{% choosable language python %}}

```python
import pulumi
import pulumi_mailslurp as mailslurp

config = pulumi.Config()

team = mailslurp.Provider("team", api_key=config.require_secret("teamApiKey"))

support = mailslurp.Inbox("support",
    name="support",
    opts=pulumi.ResourceOptions(provider=team))
```

{{% /choosable %}}

{{% choosable language csharp %}}

```csharp
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() =>
{
    var config = new Config();

    var team = new Mailslurp.Provider("team", new()
    {
        ApiKey = config.RequireSecret("teamApiKey"),
    });

    var support = new Mailslurp.Inbox("support", new()
    {
        Name = "support",
    }, new CustomResourceOptions { Provider = team });
});
```

{{% /choosable %}}

{{% choosable language go %}}

```go
package main

import (
	"github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		team, err := mailslurp.NewProvider(ctx, "team", &mailslurp.ProviderArgs{
			ApiKey: config.RequireSecret(ctx, "teamApiKey"),
		})
		if err != nil {
			return err
		}
		_, err = mailslurp.NewInbox(ctx, "support", &mailslurp.InboxArgs{
			Name: pulumi.String("support"),
		}, pulumi.Provider(team))
		return err
	})
}
```

{{% /choosable %}}

{{< /chooser >}}
