# Converted from examples/base by make examples. Change the source program, not this one.

import pulumi
import pulumi_mailslurp as mailslurp

config = pulumi.Config()
inbox_id = config.require("inboxId")
webhook_name = config.require("webhookName")
webhook_url = config.require("webhookUrl")
ruleset_target = config.require("rulesetTarget")
template_name = config.require("templateName")
read_inbox = mailslurp.get_inbox_output(inbox_id=inbox_id)
example_webhook = mailslurp.Webhook("exampleWebhook",
    name=webhook_name,
    inbox_id=inbox_id,
    url=webhook_url,
    event_name=mailslurp.WebhookEventName.NEW_EMAIL)
example_ruleset = mailslurp.InboxRuleset("exampleRuleset",
    inbox_id=inbox_id,
    scope=mailslurp.RulesetScope.RECEIVING_EMAILS,
    action=mailslurp.RulesetAction.BLOCK,
    target=ruleset_target)
example_template = mailslurp.EmailTemplate("exampleTemplate",
    name=template_name,
    content="""Hello {{firstName}},
Your inbox {{inboxAddress}} is ready.
""")
pulumi.export("emailAddress", read_inbox.email_address)
pulumi.export("webhookId", example_webhook.id)
pulumi.export("rulesetId", example_ruleset.id)
pulumi.export("templateVariables", example_template.variables)
