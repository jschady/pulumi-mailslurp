package internal

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestRulesetDetail = `{
  "id": "` + clientTestRulesetID + `",
  "inboxId": "` + clientTestInboxID + `",
  "phoneId": null,
  "scope": "RECEIVING_EMAILS",
  "action": "BLOCK",
  "target": "*@spam.example.com",
  "handler": "EXCEPTION",
  "createdAt": "2026-08-18T15:24:48.111Z"
}`

func TestClientCreateRulesetPostsWithTheInboxQueryParameter(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestRulesetDetail))

	ruleset, err := c.CreateRuleset(t.Context(), clientTestInboxID, RulesetOptions{
		Scope:  "RECEIVING_EMAILS",
		Action: "BLOCK",
		Target: "*@spam.example.com",
	})
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "/rulesets", got.Path)
	require.Equal(t, clientTestInboxID, got.Query.Get("inboxId"))

	body := got.body(t)
	require.Equal(t, "RECEIVING_EMAILS", body["scope"])
	require.Equal(t, "BLOCK", body["action"])
	require.Equal(t, "*@spam.example.com", body["target"])

	require.Equal(t, clientTestRulesetID, ruleset.ID)
	require.Equal(t, "EXCEPTION", ruleset.Handler)
	require.Nil(t, ruleset.PhoneID)
}

func TestClientGetRulesetReadsTheDetailEndpoint(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestRulesetDetail))

	ruleset, err := c.GetRuleset(t.Context(), clientTestRulesetID)
	require.NoError(t, err)

	require.Equal(t, "/rulesets/"+clientTestRulesetID, rec.only(t).Path)
	require.Equal(t, clientTestInboxID, *ruleset.InboxID)
}

func TestClientDeleteRuleset(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusNoContent, ""))

	require.NoError(t, c.DeleteRuleset(t.Context(), clientTestRulesetID))

	got := rec.only(t)
	require.Equal(t, http.MethodDelete, got.Method)
	require.Equal(t, "/rulesets/"+clientTestRulesetID, got.Path)
}

func TestClientRulesetSourceNeverWiresTheTestEndpoints(t *testing.T) {
	raw, err := readClientSource("client_rulesets.go")
	require.NoError(t, err)

	require.NotContains(t, raw, "MethodPatch", "PATCH /rulesets is testNewRuleset, a dry run")
	require.NotContains(t, raw, "MethodPut", "PUT /rulesets is testInboxRulesetsForInbox, a dry run")
}
