{{% examples %}}
## Example Usage
{{% example %}}
### A forwarder that sends every invoice email to the billing address

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const billingInbox = new mailslurp.Inbox("billing-inbox", {name: "billing-inbox"});
const invoiceForwarder = new mailslurp.InboxForwarder("invoice-forwarder", {
    field: mailslurp.ForwarderField.Subject,
    forwardToRecipients: ["billing@example.com"],
    inboxId: billingInbox.id,
    match: "invoice",
    should: mailslurp.ForwarderShould.Contain,
});
export const forwarderId = invoiceForwarder.id;
```
```python
import pulumi
import pulumi_mailslurp as mailslurp

billing_inbox = mailslurp.Inbox("billing-inbox", name="billing-inbox")
invoice_forwarder = mailslurp.InboxForwarder("invoice-forwarder",
    field=mailslurp.ForwarderField.SUBJECT,
    forward_to_recipients=["billing@example.com"],
    inbox_id=billing_inbox.id,
    match="invoice",
    should=mailslurp.ForwarderShould.CONTAIN)
pulumi.export("forwarderId", invoice_forwarder.id)
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var billingInbox = new Mailslurp.Inbox("billing-inbox", new()
    {
        Name = "billing-inbox",
    });

    var invoiceForwarder = new Mailslurp.InboxForwarder("invoice-forwarder", new()
    {
        Field = Mailslurp.ForwarderField.Subject,
        ForwardToRecipients = new[]
        {
            "billing@example.com",
        },
        InboxId = billingInbox.Id,
        Match = "invoice",
        Should = Mailslurp.ForwarderShould.Contain,
    });

    return new Dictionary<string, object?>
    {
        ["forwarderId"] = invoiceForwarder.Id,
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
		billingInbox, err := mailslurp.NewInbox(ctx, "billing-inbox", &mailslurp.InboxArgs{
			Name: pulumi.String("billing-inbox"),
		})
		if err != nil {
			return err
		}
		invoiceForwarder, err := mailslurp.NewInboxForwarder(ctx, "invoice-forwarder", &mailslurp.InboxForwarderArgs{
			Field: mailslurp.ForwarderFieldSubject,
			ForwardToRecipients: pulumi.StringArray{
				pulumi.String("billing@example.com"),
			},
			InboxId: billingInbox.ID().ToIDOutput().ToStringOutput(),
			Match:   pulumi.String("invoice"),
			Should:  mailslurp.ForwarderShouldContain,
		})
		if err != nil {
			return err
		}
		ctx.Export("forwarderId", invoiceForwarder.ID())
		return nil
	})
}
```
{{% /example %}}
{{% /examples %}}
