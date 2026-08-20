{{% examples %}}
## Example Usage
{{% example %}}
### A template that MailSlurp fills with two variables

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const welcomeTemplate = new mailslurp.EmailTemplate("welcome-template", {
    content: `Hello {{firstName}},
Your inbox {{inboxAddress}} is ready.
`,
    name: "welcome-template",
});
export const variables = welcomeTemplate.variables;
```
```python
import pulumi
import pulumi_mailslurp as mailslurp

welcome_template = mailslurp.EmailTemplate("welcome-template",
    content="""Hello {{firstName}},
Your inbox {{inboxAddress}} is ready.
""",
    name="welcome-template")
pulumi.export("variables", welcome_template.variables)
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var welcomeTemplate = new Mailslurp.EmailTemplate("welcome-template", new()
    {
        Content = @"Hello {{firstName}},
Your inbox {{inboxAddress}} is ready.
",
        Name = "welcome-template",
    });

    return new Dictionary<string, object?>
    {
        ["variables"] = welcomeTemplate.Variables,
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
		welcomeTemplate, err := mailslurp.NewEmailTemplate(ctx, "welcome-template", &mailslurp.EmailTemplateArgs{
			Content: pulumi.String("Hello {{firstName}},\nYour inbox {{inboxAddress}} is ready.\n"),
			Name:    pulumi.String("welcome-template"),
		})
		if err != nil {
			return err
		}
		ctx.Export("variables", welcomeTemplate.Variables)
		return nil
	})
}
```
{{% /example %}}
{{% /examples %}}
