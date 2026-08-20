// Converted from examples/base by make examples. Change the source program, not this one.

package main

import (
	"github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		inboxId := cfg.Require("inboxId")
		webhookName := cfg.Require("webhookName")
		webhookUrl := cfg.Require("webhookUrl")
		rulesetTarget := cfg.Require("rulesetTarget")
		templateName := cfg.Require("templateName")
		readInbox := mailslurp.LookupInboxOutput(ctx, mailslurp.LookupInboxOutputArgs{
			InboxId: pulumi.String(inboxId),
		}, nil)
		exampleWebhook, err := mailslurp.NewWebhook(ctx, "exampleWebhook", &mailslurp.WebhookArgs{
			Name:      pulumi.String(webhookName),
			InboxId:   pulumi.String(inboxId),
			Url:       pulumi.String(webhookUrl),
			EventName: mailslurp.WebhookEventNameNewEmail,
		})
		if err != nil {
			return err
		}
		exampleRuleset, err := mailslurp.NewInboxRuleset(ctx, "exampleRuleset", &mailslurp.InboxRulesetArgs{
			InboxId: pulumi.String(inboxId),
			Scope:   mailslurp.RulesetScopeReceivingEmails,
			Action:  mailslurp.RulesetActionBlock,
			Target:  pulumi.String(rulesetTarget),
		})
		if err != nil {
			return err
		}
		exampleTemplate, err := mailslurp.NewEmailTemplate(ctx, "exampleTemplate", &mailslurp.EmailTemplateArgs{
			Name:    pulumi.String(templateName),
			Content: pulumi.String("Hello {{firstName}},\nYour inbox {{inboxAddress}} is ready.\n"),
		})
		if err != nil {
			return err
		}
		ctx.Export("emailAddress", readInbox.EmailAddress())
		ctx.Export("webhookId", exampleWebhook.ID())
		ctx.Export("rulesetId", exampleRuleset.ID())
		ctx.Export("templateVariables", exampleTemplate.Variables)
		return nil
	})
}
