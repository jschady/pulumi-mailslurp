{{% examples %}}
## Example Usage
{{% example %}}
### A ruleset that blocks the email of one domain

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const supportInbox = new mailslurp.Inbox("support-inbox", {name: "support-inbox"});
const blockOneDomain = new mailslurp.InboxRuleset("block-one-domain", {
    action: mailslurp.RulesetAction.Block,
    inboxId: supportInbox.id,
    scope: mailslurp.RulesetScope.ReceivingEmails,
    target: "*@blocked.example.com",
});
export const rulesetId = blockOneDomain.id;
```
```python
import pulumi
import pulumi_mailslurp as mailslurp

support_inbox = mailslurp.Inbox("support-inbox", name="support-inbox")
block_one_domain = mailslurp.InboxRuleset("block-one-domain",
    action=mailslurp.RulesetAction.BLOCK,
    inbox_id=support_inbox.id,
    scope=mailslurp.RulesetScope.RECEIVING_EMAILS,
    target="*@blocked.example.com")
pulumi.export("rulesetId", block_one_domain.id)
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var supportInbox = new Mailslurp.Inbox("support-inbox", new()
    {
        Name = "support-inbox",
    });

    var blockOneDomain = new Mailslurp.InboxRuleset("block-one-domain", new()
    {
        Action = Mailslurp.RulesetAction.Block,
        InboxId = supportInbox.Id,
        Scope = Mailslurp.RulesetScope.ReceivingEmails,
        Target = "*@blocked.example.com",
    });

    return new Dictionary<string, object?>
    {
        ["rulesetId"] = blockOneDomain.Id,
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
		supportInbox, err := mailslurp.NewInbox(ctx, "support-inbox", &mailslurp.InboxArgs{
			Name: pulumi.String("support-inbox"),
		})
		if err != nil {
			return err
		}
		blockOneDomain, err := mailslurp.NewInboxRuleset(ctx, "block-one-domain", &mailslurp.InboxRulesetArgs{
			Action:  mailslurp.RulesetActionBlock,
			InboxId: supportInbox.ID().ToIDOutput().ToStringOutput(),
			Scope:   mailslurp.RulesetScopeReceivingEmails,
			Target:  pulumi.String("*@blocked.example.com"),
		})
		if err != nil {
			return err
		}
		ctx.Export("rulesetId", blockOneDomain.ID())
		return nil
	})
}
```
{{% /example %}}
{{% /examples %}}
