//go:build yaml || all

package examples

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi/pkg/v3/testing/integration"
)

// The programs written in Pulumi YAML: the base program with its update, the one program that
// creates an inbox, and the complete program this account plan cannot deploy.

func TestTheBaseProgramInYaml(t *testing.T) {
	opts := baseOptions(t).With(baseProgramOptions(t, baseSource))
	opts = opts.With(integration.ProgramTestOptions{
		EditDirs: []integration.EditDir{{
			Dir:                    filepath.Join(cwd(t), "base-update"),
			Additive:               true,
			ExtraRuntimeValidation: requireTheUpdatedTemplate,
		}},
	})
	runProgram(t, opts)
}

// requireTheUpdatedTemplate reads the update leg. The added variable proves the engine sent the
// old inputs to the provider, and that the provider answered an update rather than a replacement.
func requireTheUpdatedTemplate(t *testing.T, stack integration.RuntimeValidationStackInfo) {
	variables, ok := stack.Outputs["templateVariables"].([]any)
	require.Truef(t, ok, "the template variables should be a list, and they are a %T",
		stack.Outputs["templateVariables"])
	assert.Len(t, variables, 3, "the updated template content declares three variables")
}

// TestTheInboxProgram is the one leg that creates an inbox. The description change is an update
// that no property forces a replacement for, so the leg pays for exactly one inbox.
func TestTheInboxProgram(t *testing.T) {
	key := requireAPIKey(t)
	requireInboxBudget(t, "inbox", 1)

	name := newTestName(inboxKind)
	t.Cleanup(func() { sweepLeg(t, key, map[string]string{inboxClass: name}) })

	var created string
	opts := baseOptions(t).With(integration.ProgramTestOptions{
		Dir:    filepath.Join(cwd(t), "inbox"),
		Config: map[string]string{"inboxName": name},
		ExtraRuntimeValidation: func(t *testing.T, stack integration.RuntimeValidationStackInfo) {
			surveyTheAccount(t, key, "while the inbox program is deployed")
			chargeInboxes(t, countInboxResources(stack), "inbox")
			created = requireTheInboxOnTheAccount(t, stack, key, "The inbox this program creates")
		},
		EditDirs: []integration.EditDir{{
			Dir:      filepath.Join(cwd(t), "inbox-update"),
			Additive: true,
			ExtraRuntimeValidation: func(t *testing.T, stack integration.RuntimeValidationStackInfo) {
				updated := requireTheInboxOnTheAccount(t, stack, key, "The inbox this program updated")
				assert.Equal(t, created, updated,
					"the description change is an update, so the inbox keeps its identifier")
			},
		}},
	})
	runProgram(t, opts)
}

// requireTheInboxOnTheAccount reads the deployed inbox back from MailSlurp and answers its id.
func requireTheInboxOnTheAccount(t *testing.T, stack integration.RuntimeValidationStackInfo,
	key, wantDescription string,
) string {
	t.Helper()
	inboxID := requireStringOutput(t, stack, "inboxId")
	body := requireObjectExists(t, key, "/inboxes/"+inboxID, "inbox")

	var read struct {
		Description  *string `json:"description"`
		EmailAddress string  `json:"emailAddress"`
	}
	require.NoError(t, json.Unmarshal(body, &read))
	require.NotNil(t, read.Description, "the account should hold the description of the inbox")
	assert.Equal(t, wantDescription, *read.Description)
	assert.Equal(t, read.EmailAddress, requireStringOutput(t, stack, "emailAddress"),
		"the stack should export the address the account gives the inbox")
	return inboxID
}

// TestTheCompleteProgram deploys every resource and both functions. It runs on an account whose
// plan carries the inbox forwarder and that holds a domain, and skips loudly on any other.
func TestTheCompleteProgram(t *testing.T) {
	key := requireAPIKey(t)
	requireTheCompletePreconditions(t, key, theSharedInbox(t))
	requireInboxBudget(t, completeSource, 1)

	inboxName := newTestName(inboxKind)
	webhookName := newTestName(webhookKind)
	rulesetTarget := rulesetTargetFor(newTestName(rulesetKind))
	templateName := newTestName(templateKind)
	recipient := forwarderRecipientFor(newTestName(forwarderKind))

	t.Cleanup(func() {
		sweepLeg(t, key, map[string]string{
			inboxClass: inboxName, webhookClass: webhookName, rulesetClass: rulesetTarget,
			templateClass: templateName, forwarderClass: recipient,
		})
	})

	opts := baseOptions(t).With(integration.ProgramTestOptions{
		Dir: filepath.Join(cwd(t), completeSource),
		Config: map[string]string{
			"inboxName": inboxName, "webhookName": webhookName, "webhookUrl": webhookURLFor(webhookName),
			"rulesetTarget": rulesetTarget, "templateName": templateName,
			"forwarderRecipient": recipient, "domainId": theFirstDomainID(t, key),
		},
		ExtraRuntimeValidation: func(t *testing.T, stack integration.RuntimeValidationStackInfo) {
			surveyTheAccount(t, key, "while the complete program is deployed")
			chargeInboxes(t, countInboxResources(stack), completeSource)
			requireObjectExists(t, key,
				"/forwarders/"+requireStringOutput(t, stack, "forwarderId"), "forwarder")
			assert.NotEmpty(t, requireStringOutput(t, stack, "domainName"),
				"the domain function should answer the name of the domain")
		},
	})
	runProgram(t, opts)
}
