package internal

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestForwarderDetail = `{
  "id": "` + clientTestForwarderID + `",
  "inboxId": "` + clientTestInboxID + `",
  "name": "the server names it",
  "field": "SUBJECT",
  "match": "invoice*",
  "should": "WILDCARD",
  "forwardToRecipients": ["billing@example.com"],
  "matchOptions": {
    "operator": "AND",
    "matches": [{"field":"SENDER","should":"CONTAIN","value":"example.com"}],
    "groups": null
  },
  "createdAt": "2026-08-18T15:24:48.111Z"
}`

func clientTestForwarderOptions() ForwarderOptions {
	return ForwarderOptions{
		Field:               clientTestPtr("SUBJECT"),
		Should:              clientTestPtr("WILDCARD"),
		Match:               clientTestPtr("invoice*"),
		ForwardToRecipients: []string{"billing@example.com"},
	}
}

func TestClientCreateForwarderPostsWithTheInboxQueryParameter(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestForwarderDetail))

	forwarder, err := c.CreateForwarder(t.Context(), clientTestInboxID, clientTestForwarderOptions())
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "/forwarders", got.Path)
	require.Equal(t, clientTestInboxID, got.Query.Get("inboxId"))
	require.Equal(t, []any{"billing@example.com"}, got.body(t)["forwardToRecipients"])

	require.Equal(t, clientTestForwarderID, forwarder.ID)
	require.Equal(t, "the server names it", *forwarder.Name)
}

func TestClientGetForwarderReadsTheDetailEndpoint(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestForwarderDetail))

	forwarder, err := c.GetForwarder(t.Context(), clientTestForwarderID)
	require.NoError(t, err)

	require.Equal(t, "/forwarders/"+clientTestForwarderID, rec.only(t).Path)
	require.NotNil(t, forwarder.MatchOptions)
	require.Equal(t, "AND", forwarder.MatchOptions.Operator)
	require.Len(t, forwarder.MatchOptions.Matches, 1)
	require.Equal(t, "SENDER", forwarder.MatchOptions.Matches[0].Field)
	require.Equal(t, "example.com", forwarder.MatchOptions.Matches[0].Value)
}

func TestClientUpdateForwarderPutsTheWholeOptionsBody(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestForwarderDetail))

	opts := clientTestForwarderOptions()
	opts.MatchOptions = &ForwarderMatchOptions{
		Operator: "OR",
		Matches:  []ForwarderMatch{{Field: "SENDER", Should: "EQUAL", Value: "a@example.com"}},
	}
	_, err := c.UpdateForwarder(t.Context(), clientTestForwarderID, opts)
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPut, got.Method)
	require.Equal(t, "/forwarders/"+clientTestForwarderID, got.Path)

	body := got.body(t)
	for _, key := range []string{"field", "should", "match", "forwardToRecipients", "matchOptions"} {
		require.Contains(t, body, key, "the update takes the whole create options body")
	}
}

func TestClientDeleteForwarder(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusNoContent, ""))

	require.NoError(t, c.DeleteForwarder(t.Context(), clientTestForwarderID))

	got := rec.only(t)
	require.Equal(t, http.MethodDelete, got.Method)
	require.Equal(t, "/forwarders/"+clientTestForwarderID, got.Path)
}

func TestClientForwarderOptionsCarryNoName(t *testing.T) {
	options := reflect.TypeOf(ForwarderOptions{})

	_, found := options.FieldByName("Name")
	require.False(t, found, "the create options have no name, so you cannot set it")

	read := reflect.TypeOf(ForwarderDto{})
	_, found = read.FieldByName("Name")
	require.True(t, found, "the read model does carry the name the server chose")
}
