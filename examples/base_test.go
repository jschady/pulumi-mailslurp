//go:build yaml || nodejs || python || dotnet || go || all

package examples

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi/pkg/v3/testing/integration"
)

// The program every language leg deploys. It attaches to the shared inbox, so a leg of any
// language creates no inbox of its own.

// webhookURLFor belongs to the reserved documentation domain, so no real server is ever called.
// The name rides the path, because the API refuses a second webhook of one inbox on the same URL.
func webhookURLFor(name string) string { return "https://example.com/mailslurp/" + name }

func cwd(t *testing.T) string {
	t.Helper()
	here, err := os.Getwd()
	require.NoError(t, err)
	return here
}

// requireObjectExists reads one object back from the account. A stack output alone would pass
// while the provider answered an identifier the account never gave it.
func requireObjectExists(t *testing.T, key, path, what string) []byte {
	t.Helper()
	status, body, err := apiCall(context.Background(), key, http.MethodGet, path, nil, nil)
	require.NoErrorf(t, err, "the account should answer for the %s", what)
	require.Equalf(t, http.StatusOK, status, "the account should hold the %s", what)
	return body
}

func requireStringOutput(t *testing.T, stack integration.RuntimeValidationStackInfo, name string) string {
	t.Helper()
	value, ok := stack.Outputs[name].(string)
	require.Truef(t, ok, "the %s output should be a string, and it is a %T", name, stack.Outputs[name])
	require.NotEmptyf(t, value, "the %s output should carry a value", name)
	return value
}

// sweepLeg removes what one leg left behind and reports it. The stack destroy of the program test
// runs first, so a removal here is a leak rather than ordinary cleanup.
func sweepLeg(t *testing.T, key string, markers map[string]string) {
	t.Helper()
	ctx := context.Background()
	for _, class := range accountClasses() {
		marker, wanted := markers[class.name]
		if !wanted {
			continue
		}
		if removed := class.sweepMarked(ctx, key, marker); removed > 0 {
			t.Errorf("the program left %d %s behind, which the sweep has now removed", removed, class.name)
		}
	}
}

// baseProgramOptions gives one base program the shared inbox, a name per object that the sweeps
// match, and the cleanup that runs when the program test fails before its own destroy.
func baseProgramOptions(t *testing.T, program string) integration.ProgramTestOptions {
	t.Helper()
	key := requireAPIKey(t)
	// The source declares no inbox, so no leg of any language pays for one.
	requireInboxBudget(t, baseSource, 0)

	inboxID := theSharedInbox(t)
	webhookName := newTestName(webhookKind)
	rulesetTarget := rulesetTargetFor(newTestName(rulesetKind))
	templateName := newTestName(templateKind)

	t.Cleanup(func() {
		sweepLeg(t, key, map[string]string{
			webhookClass:  webhookName,
			rulesetClass:  rulesetTarget,
			templateClass: templateName,
		})
	})

	return integration.ProgramTestOptions{
		Dir: filepath.Join(cwd(t), program),
		Config: map[string]string{
			"inboxId":       inboxID,
			"webhookName":   webhookName,
			"webhookUrl":    webhookURLFor(webhookName),
			"rulesetTarget": rulesetTarget,
			"templateName":  templateName,
		},
		ExtraRuntimeValidation: func(t *testing.T, stack integration.RuntimeValidationStackInfo) {
			requireTheBaseProgramOnTheAccount(t, stack, program, key, inboxID)
		},
	}
}

// requireTheBaseProgramOnTheAccount reads every object of the deployed program back from MailSlurp.
func requireTheBaseProgramOnTheAccount(t *testing.T, stack integration.RuntimeValidationStackInfo,
	program, key, inboxID string,
) {
	t.Helper()
	surveyTheAccount(t, key, "while the "+program+" program is deployed")
	chargeInboxes(t, countInboxResources(stack), program)
	assert.Zerof(t, countInboxResources(stack), "the %s program should hold no inbox", program)

	inbox := requireObjectExists(t, key, "/inboxes/"+inboxID, "shared inbox")
	var read struct {
		EmailAddress string `json:"emailAddress"`
	}
	require.NoError(t, json.Unmarshal(inbox, &read))
	assert.Equal(t, read.EmailAddress, requireStringOutput(t, stack, "emailAddress"),
		"the inbox function should answer the address the account gives the inbox")

	requireObjectExists(t, key, "/webhooks/"+requireStringOutput(t, stack, "webhookId"), "webhook")
	requireObjectExists(t, key, "/rulesets/"+requireStringOutput(t, stack, "rulesetId"), "ruleset")

	variables, ok := stack.Outputs["templateVariables"].([]any)
	require.Truef(t, ok, "the template variables should be a list, and they are a %T",
		stack.Outputs["templateVariables"])
	assert.Len(t, variables, 2, "the template content declares two variables")
}
