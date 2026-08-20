package internal

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestInboxDetail = `{
  "id": "` + clientTestInboxID + `",
  "createdAt": "2026-08-18T15:24:48.111Z",
  "name": "bs1",
  "domainId": null,
  "description": "bs1 description",
  "emailAddress": "0690943a@mailslurp.net",
  "expiresAt": null,
  "favourite": false,
  "tags": ["a", "b"],
  "inboxType": "HTTP_INBOX",
  "readOnly": false,
  "virtualInbox": false,
  "functionsAs": null,
  "localPart": "0690943a",
  "domain": "mailslurp.net",
  "accountRegion": "US_WEST_2"
}`

func TestClientCreateInboxPostsTheOptionsBody(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestInboxDetail))

	inbox, err := c.CreateInbox(t.Context(), CreateInboxOptions{
		Name:            clientTestPtr("bs1"),
		Description:     clientTestPtr("bs1 description"),
		Tags:            []string{"a", "b"},
		InboxType:       clientTestPtr("HTTP_INBOX"),
		UseShortAddress: clientTestPtr(true),
		ExpiresIn:       clientTestPtr(int64(3600000)),
	})
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "/inboxes/withOptions", got.Path, "the query-parameter form is not used")

	body := got.body(t)
	require.Equal(t, "bs1", body["name"])
	require.Equal(t, []any{"a", "b"}, body["tags"])
	require.Equal(t, "HTTP_INBOX", body["inboxType"])
	require.Equal(t, true, body["useShortAddress"])
	require.NotContains(t, body, "allowTeamAccess", "the API marks it deprecated")

	require.Equal(t, clientTestInboxID, inbox.ID)
	require.Equal(t, "0690943a@mailslurp.net", inbox.EmailAddress)
	require.NotNil(t, inbox.InboxType)
	require.Equal(t, "HTTP_INBOX", *inbox.InboxType)
	require.Equal(t, []string{"a", "b"}, inbox.Tags)
	require.NotNil(t, inbox.AccountRegion)
	require.Equal(t, "US_WEST_2", *inbox.AccountRegion)
}

func TestClientCreateInboxOmitsEveryUnsetOption(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestInboxDetail))

	_, err := c.CreateInbox(t.Context(), CreateInboxOptions{})
	require.NoError(t, err)

	require.Equal(t, "{}", string(rec.only(t).Body), "an unset option must not travel as null")
}

func TestClientGetInboxReadsTheDetailEndpoint(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestInboxDetail))

	inbox, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodGet, got.Method)
	require.Equal(t, "/inboxes/"+clientTestInboxID, got.Path,
		"the list projection returns a null inboxType, so Read never uses it")
	require.Equal(t, "mailslurp.net", *inbox.Domain)
}

func TestClientGetInboxKeepsANullInboxType(t *testing.T) {
	c, _ := clientTestServer(t, clientTestReply(http.StatusOK, `{"id":"x","inboxType":null}`))

	inbox, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)
	require.Nil(t, inbox.InboxType, "null must stay distinguishable from a value")
}

func TestClientUpdateInboxOmitsEveryUnchangedProperty(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestInboxDetail))

	_, err := c.UpdateInbox(t.Context(), clientTestInboxID, UpdateInboxOptions{
		Name: clientTestPtr("renamed"),
	})
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPatch, got.Method)
	require.Equal(t, "/inboxes/"+clientTestInboxID, got.Path)

	body := got.body(t)
	require.Equal(t, "renamed", body["name"])
	// The endpoint merges, and it swallows an explicit null with a lying 200.
	require.NotContains(t, body, "description")
	require.NotContains(t, body, "tags")
	require.NotContains(t, body, "expiresAt")
	require.NotContains(t, body, "favourite")
}

func TestClientUpdateInboxSendsNothingWhenNothingChanged(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestInboxDetail))

	_, err := c.UpdateInbox(t.Context(), clientTestInboxID, UpdateInboxOptions{})
	require.NoError(t, err)

	require.Equal(t, "{}", string(rec.only(t).Body), "a null clears nothing and reports success")
}

func TestClientUpdateInboxClearsTagsWithAnEmptyList(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestInboxDetail))

	_, err := c.UpdateInbox(t.Context(), clientTestInboxID, UpdateInboxOptions{
		Tags: &[]string{},
	})
	require.NoError(t, err)

	body := rec.only(t).body(t)
	require.Equal(t, []any{}, body["tags"], "an empty list clears the tags; a null does not")
}

func TestClientUpdateInboxSendsTheFalseFavourite(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestInboxDetail))

	_, err := c.UpdateInbox(t.Context(), clientTestInboxID, UpdateInboxOptions{
		Favourite: clientTestPtr(false),
	})
	require.NoError(t, err)

	require.Equal(t, false, rec.only(t).body(t)["favourite"])
}

func TestClientDeleteInboxReportsAGoneInbox(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusNotFound, clientTestNotFoundBody))

	err := c.DeleteInbox(t.Context(), clientTestInboxID)
	require.True(t, IsGone(err), "the resource decides what a gone inbox means, not the client")
	require.Equal(t, "/inboxes/"+clientTestInboxID, rec.only(t).Path)
}
