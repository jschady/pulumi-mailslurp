// Converted from examples/base by make examples. Change the source program, not this one.

using System.Collections.Generic;
using System.Linq;
using Pulumi;
using Mailslurp = Jschady.Mailslurp;

return await Deployment.RunAsync(() => 
{
    var config = new Config();
    var inboxId = config.Require("inboxId");
    var webhookName = config.Require("webhookName");
    var webhookUrl = config.Require("webhookUrl");
    var rulesetTarget = config.Require("rulesetTarget");
    var templateName = config.Require("templateName");
    var readInbox = Mailslurp.GetInbox.Invoke(new()
    {
        InboxId = inboxId,
    });

    var exampleWebhook = new Mailslurp.Webhook("exampleWebhook", new()
    {
        Name = webhookName,
        InboxId = inboxId,
        Url = webhookUrl,
        EventName = Mailslurp.WebhookEventName.NewEmail,
    });

    var exampleRuleset = new Mailslurp.InboxRuleset("exampleRuleset", new()
    {
        InboxId = inboxId,
        Scope = Mailslurp.RulesetScope.ReceivingEmails,
        Action = Mailslurp.RulesetAction.Block,
        Target = rulesetTarget,
    });

    var exampleTemplate = new Mailslurp.EmailTemplate("exampleTemplate", new()
    {
        Name = templateName,
        Content = @"Hello {{firstName}},
Your inbox {{inboxAddress}} is ready.
",
    });

    return new Dictionary<string, object?>
    {
        ["emailAddress"] = readInbox.Apply(getInboxResult => getInboxResult.EmailAddress),
        ["webhookId"] = exampleWebhook.Id,
        ["rulesetId"] = exampleRuleset.Id,
        ["templateVariables"] = exampleTemplate.Variables,
    };
});

