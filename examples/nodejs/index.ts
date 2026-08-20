// Converted from examples/base by make examples. Change the source program, not this one.

import * as pulumi from "@pulumi/pulumi";
import * as mailslurp from "pulumi-mailslurp";

const config = new pulumi.Config();
const inboxId = config.require("inboxId");
const webhookName = config.require("webhookName");
const webhookUrl = config.require("webhookUrl");
const rulesetTarget = config.require("rulesetTarget");
const templateName = config.require("templateName");
const readInbox = mailslurp.getInboxOutput({
    inboxId: inboxId,
});
const exampleWebhook = new mailslurp.Webhook("exampleWebhook", {
    name: webhookName,
    inboxId: inboxId,
    url: webhookUrl,
    eventName: mailslurp.WebhookEventName.NewEmail,
});
const exampleRuleset = new mailslurp.InboxRuleset("exampleRuleset", {
    inboxId: inboxId,
    scope: mailslurp.RulesetScope.ReceivingEmails,
    action: mailslurp.RulesetAction.Block,
    target: rulesetTarget,
});
const exampleTemplate = new mailslurp.EmailTemplate("exampleTemplate", {
    name: templateName,
    content: `Hello {{firstName}},
Your inbox {{inboxAddress}} is ready.
`,
});
export const emailAddress = readInbox.emailAddress;
export const webhookId = exampleWebhook.id;
export const rulesetId = exampleRuleset.id;
export const templateVariables = exampleTemplate.variables;
