{{% examples %}}
## Example Usage
{{% example %}}
### An inbox that receives email at an address MailSlurp assigns

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const teamInbox = new mailslurp.Inbox("team-inbox", {
    description: "The shared inbox of the team",
    inboxType: mailslurp.InboxType.HttpInbox,
    name: "team-inbox",
    tags: [
        "pulumi",
        "team",
    ],
});
export const emailAddress = teamInbox.emailAddress;
```
```python
import pulumi
import pulumi_mailslurp as mailslurp

team_inbox = mailslurp.Inbox("team-inbox",
    description="The shared inbox of the team",
    inbox_type=mailslurp.InboxType.HTTP_INBOX,
    name="team-inbox",
    tags=[
        "pulumi",
        "team",
    ])
pulumi.export("emailAddress", team_inbox.email_address)
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var teamInbox = new Mailslurp.Inbox("team-inbox", new()
    {
        Description = "The shared inbox of the team",
        InboxType = Mailslurp.InboxType.HttpInbox,
        Name = "team-inbox",
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
```go
package main

import (
	"github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		teamInbox, err := mailslurp.NewInbox(ctx, "team-inbox", &mailslurp.InboxArgs{
			Description: pulumi.String("The shared inbox of the team"),
			InboxType:   mailslurp.InboxTypeHttpInbox,
			Name:        pulumi.String("team-inbox"),
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
{{% /example %}}
{{% example %}}
### A virtual inbox that holds every test email inside MailSlurp

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const testInbox = new mailslurp.Inbox("test-inbox", {
    description: "The inbox that the acceptance tests read",
    expiresIn: 3600000,
    name: "test-inbox",
    tags: ["test"],
    virtualInbox: true,
});
export const emailAddress = testInbox.emailAddress;
```
```python
import pulumi
import pulumi_mailslurp as mailslurp

test_inbox = mailslurp.Inbox("test-inbox",
    description="The inbox that the acceptance tests read",
    expires_in=3600000,
    name="test-inbox",
    tags=["test"],
    virtual_inbox=True)
pulumi.export("emailAddress", test_inbox.email_address)
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var testInbox = new Mailslurp.Inbox("test-inbox", new()
    {
        Description = "The inbox that the acceptance tests read",
        ExpiresIn = 3600000,
        Name = "test-inbox",
        Tags = new[]
        {
            "test",
        },
        VirtualInbox = true,
    });

    return new Dictionary<string, object?>
    {
        ["emailAddress"] = testInbox.EmailAddress,
    };
});

```
```go
package main

import (
	"github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		testInbox, err := mailslurp.NewInbox(ctx, "test-inbox", &mailslurp.InboxArgs{
			Description: pulumi.String("The inbox that the acceptance tests read"),
			ExpiresIn:   pulumi.Int(3600000),
			Name:        pulumi.String("test-inbox"),
			Tags: pulumi.StringArray{
				pulumi.String("test"),
			},
			VirtualInbox: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}
		ctx.Export("emailAddress", testInbox.EmailAddress)
		return nil
	})
}
```
{{% /example %}}
{{% /examples %}}
