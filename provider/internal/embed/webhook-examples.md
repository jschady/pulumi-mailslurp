{{% examples %}}
## Example Usage
{{% example %}}
### A webhook that posts every new email of one inbox to your server

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const alertInbox = new mailslurp.Inbox("alert-inbox", {name: "alert-inbox"});
const newEmailHook = new mailslurp.Webhook("new-email-hook", {
    eventName: mailslurp.WebhookEventName.NewEmail,
    inboxId: alertInbox.id,
    name: "new-email-hook",
    url: "https://example.com/mailslurp/new-email",
});
export const webhookId = newEmailHook.id;
```
```python
import pulumi
import pulumi_mailslurp as mailslurp

alert_inbox = mailslurp.Inbox("alert-inbox", name="alert-inbox")
new_email_hook = mailslurp.Webhook("new-email-hook",
    event_name=mailslurp.WebhookEventName.NEW_EMAIL,
    inbox_id=alert_inbox.id,
    name="new-email-hook",
    url="https://example.com/mailslurp/new-email")
pulumi.export("webhookId", new_email_hook.id)
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var alertInbox = new Mailslurp.Inbox("alert-inbox", new()
    {
        Name = "alert-inbox",
    });

    var newEmailHook = new Mailslurp.Webhook("new-email-hook", new()
    {
        EventName = Mailslurp.WebhookEventName.NewEmail,
        InboxId = alertInbox.Id,
        Name = "new-email-hook",
        Url = "https://example.com/mailslurp/new-email",
    });

    return new Dictionary<string, object?>
    {
        ["webhookId"] = newEmailHook.Id,
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
		alertInbox, err := mailslurp.NewInbox(ctx, "alert-inbox", &mailslurp.InboxArgs{
			Name: pulumi.String("alert-inbox"),
		})
		if err != nil {
			return err
		}
		newEmailHook, err := mailslurp.NewWebhook(ctx, "new-email-hook", &mailslurp.WebhookArgs{
			EventName: mailslurp.WebhookEventNameNewEmail,
			InboxId:   alertInbox.ID().ToIDOutput().ToStringOutput(),
			Name:      pulumi.String("new-email-hook"),
			Url:       pulumi.String("https://example.com/mailslurp/new-email"),
		})
		if err != nil {
			return err
		}
		ctx.Export("webhookId", newEmailHook.ID())
		return nil
	})
}
```
{{% /example %}}
{{% /examples %}}
