package examples

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two account preconditions of the complete program, and the pins that keep each skip honest.
// This file carries no build tag, so the pins run without a credential.

// errorCodeSubscriptionLimit is what MailSlurp answers when the account plan carries no forwarder.
const errorCodeSubscriptionLimit = "W_429_SUBSCRIPTION_LIMIT_REACHED"

const noForwarderEntitlementReason = "the MailSlurp account plan does not carry the inbox " +
	"forwarder feature, so the complete program cannot be deployed here"

const noDomainReason = "this MailSlurp account holds no domain: add a verified domain to run " +
	"the complete program"

// skipReasonForForwarderEntitlement answers the reason to skip, or the empty string when the
// account creates a forwarder. Only the one plan-refusal code skips; every other answer fails.
func skipReasonForForwarderEntitlement(errorCode string) string {
	if errorCode == errorCodeSubscriptionLimit {
		return noForwarderEntitlementReason
	}
	return ""
}

// skipReasonForDomains answers the reason to skip, or the empty string when the account holds one.
func skipReasonForDomains(found int) string {
	if found == 0 {
		return noDomainReason
	}
	return ""
}

// probeTheForwarderEntitlement asks the account for one forwarder and removes it again. MailSlurp
// gates the create alone, so no read answers the question and the probe has to write.
func probeTheForwarderEntitlement(t *testing.T, key, inboxID string) string {
	t.Helper()
	ctx := context.Background()
	recipient := forwarderRecipientFor(newTestName(forwarderKind))

	// The sweep is registered before the create, so a forwarder the API made and then reported as
	// a failure is still removed.
	t.Cleanup(func() {
		for _, class := range accountClasses() {
			if class.name == forwarderClass {
				class.sweepMarked(context.WithoutCancel(ctx), key, recipient)
			}
		}
	})

	// The API refuses a body that names neither a field and a match nor a match tree.
	body, err := json.Marshal(map[string]any{
		"field":               "SUBJECT",
		"match":               "invoice",
		"forwardToRecipients": []string{recipient},
	})
	require.NoError(t, err)

	query := url.Values{}
	query.Set("inboxId", inboxID)
	status, answered, err := apiCall(ctx, key, http.MethodPost, "/forwarders", query, body)
	require.NoError(t, err, "the entitlement probe could not reach the API")

	if status == http.StatusOK || status == http.StatusCreated {
		var created struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(answered, &created))
		require.NotEmpty(t, created.ID, "the entitlement probe captured no identifier")
		_, _, err = apiCall(ctx, key, http.MethodDelete, "/forwarders/"+created.ID, nil, nil)
		require.NoError(t, err)
		return ""
	}

	var refusal struct {
		ErrorCode string `json:"errorCode"`
	}
	require.NoError(t, json.Unmarshal(answered, &refusal))
	reason := skipReasonForForwarderEntitlement(refusal.ErrorCode)
	require.NotEmptyf(t, reason,
		"the entitlement probe failed for another reason: status %d, code %q",
		status, refusal.ErrorCode)
	return reason
}

// probeTheDomains asks the account how many domains it holds.
func probeTheDomains(t *testing.T, key string) string {
	t.Helper()
	for _, class := range accountClasses() {
		if class.name != domainClass {
			continue
		}
		found, err := class.list(context.Background(), key)
		require.NoError(t, err, "the domain probe could not read the account")
		return skipReasonForDomains(len(found))
	}
	return noDomainReason
}

// theFirstDomainID answers a domain the account holds. Only a leg that already passed the domain
// probe calls it.
func theFirstDomainID(t *testing.T, key string) string {
	t.Helper()
	for _, class := range accountClasses() {
		if class.name != domainClass {
			continue
		}
		found, err := class.list(context.Background(), key)
		require.NoError(t, err)
		require.NotEmpty(t, found, "the domain probe passed, so the account holds a domain")
		return found[0].ID
	}
	return ""
}

// requireTheCompletePreconditions skips the complete program when the account refuses a forwarder
// or holds no domain, and names the one it found missing.
func requireTheCompletePreconditions(t *testing.T, key, inboxID string) {
	t.Helper()
	if reason := probeTheForwarderEntitlement(t, key, inboxID); reason != "" {
		t.Skip(reason)
	}
	if reason := probeTheDomains(t, key); reason != "" {
		t.Skip(reason)
	}
}

// An entitled account must deploy the complete program. Only the one plan-refusal code skips it,
// and this pin holds that decision, because no test can buy the feature to prove it live.
func TestTheCompleteProgramSkipsOnlyWhenThePlanRefusesTheForwarder(t *testing.T) {
	t.Parallel()
	assert.Equal(t, noForwarderEntitlementReason,
		skipReasonForForwarderEntitlement(errorCodeSubscriptionLimit),
		"the one plan-refusal code skips the program and names itself")

	assert.Empty(t, skipReasonForForwarderEntitlement(""),
		"an entitled account must deploy the complete program")
	assert.Empty(t, skipReasonForForwarderEntitlement("W_400_BAD_REQUEST"),
		"any other refusal is a failure, not a skip")
}

// An account that holds a domain must deploy the complete program, so only an empty list skips it.
func TestTheCompleteProgramSkipsOnlyWhenTheAccountHoldsNoDomain(t *testing.T) {
	t.Parallel()
	assert.Equal(t, noDomainReason, skipReasonForDomains(0))
	assert.Empty(t, skipReasonForDomains(1), "one domain is enough to deploy the complete program")
	assert.Empty(t, skipReasonForDomains(7))
}
