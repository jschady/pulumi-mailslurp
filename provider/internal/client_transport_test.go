package internal

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientSetsTheJSONContentTypeOnlyWhenItSendsABody(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, `{"id":"`+clientTestInboxID+`"}`))

	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)
	require.Empty(t, rec.at(t, 0).Header.Get("Content-Type"), "a GET carries no body")

	_, err = c.CreateInbox(t.Context(), CreateInboxOptions{Name: clientTestPtr("probe")})
	require.NoError(t, err)
	require.Equal(t, clientTestJSONType, rec.at(t, 1).Header.Get("Content-Type"))
}

func TestClientAcceptsEveryTwoHundredStatus(t *testing.T) {
	// Verified live: inbox delete 204, webhook delete 200, create 201, alias update 202.
	statuses := []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent}

	for _, status := range statuses {
		c, _ := clientTestServer(t, clientTestReply(status, ""))
		require.NoError(t, c.DeleteInbox(t.Context(), clientTestInboxID),
			"status %d is a success", status)
	}
}

func TestClientReturnsNoErrorForAnEmptyBodyOnADelete(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusNoContent, ""))

	require.NoError(t, c.DeleteInbox(t.Context(), clientTestInboxID))
	require.Equal(t, http.MethodDelete, rec.only(t).Method)
}

func TestClientReportsAResponseThatIsNotJSON(t *testing.T) {
	c, _ := clientTestServer(t, clientTestReply(http.StatusOK, `<html>not json</html>`))

	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "getInbox", "the error must name the operation")
	require.NotContains(t, err.Error(), clientTestKey)
}

func TestClientStopsWhenTheContextIsAlreadyCancelled(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, `{}`))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.GetInbox(ctx, clientTestInboxID)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, rec.count(), "a cancelled context sends nothing")
}

func TestClientEscapesAnIdentifierInThePath(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, `{"id":"x"}`))

	_, err := c.GetInbox(t.Context(), "../webhooks/other")
	require.NoError(t, err)

	got := rec.only(t)
	require.True(t, strings.HasPrefix(got.RequestURI, "/inboxes/"),
		"an identifier must not retarget the request: %s", got.RequestURI)
	require.Contains(t, got.RequestURI, "%2F", "a slash in an identifier must be escaped")
}
