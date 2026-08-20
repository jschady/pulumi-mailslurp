package internal

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestPagePath = "/inboxes/paginated"

// clientTestPage renders the Spring page envelope the API really returns, including the two
// counters that are permanently -1 and the `last` flag that stays false past the end.
func clientTestPage(content string, empty bool) string {
	return fmt.Sprintf(`{"content":[%s],"totalPages":-1,"totalElements":-1,`+
		`"last":false,"size":20,"numberOfElements":1,"first":true,"empty":%t}`, content, empty)
}

func clientTestWalk(t *testing.T, handler http.HandlerFunc) ([]InboxDto, *clientTestRecorder, error) {
	t.Helper()
	c, rec := clientTestServer(t, handler)

	got, err := walkPages[InboxDto](t.Context(), c, "getAllInboxes", clientTestPagePath, nil)
	return got, rec, err
}

func TestPaginationTerminatesOnEmptyAndIgnoresTheCounters(t *testing.T) {
	got, rec, err := clientTestWalk(t, clientTestSequence(
		clientTestReply(http.StatusOK, clientTestPage(`{"id":"one"},{"id":"two"}`, false)),
		clientTestReply(http.StatusOK, clientTestPage(`{"id":"three"}`, false)),
		clientTestReply(http.StatusOK, clientTestPage("", true)),
	))

	require.NoError(t, err)
	require.Equal(t, []string{"one", "two", "three"}, []string{got[0].ID, got[1].ID, got[2].ID})
	require.Equal(t, 3, rec.count(), "one wasted request ends every walk")
}

func TestPaginationTerminatesOnEmptyContentWithoutTheEmptyFlag(t *testing.T) {
	got, rec, err := clientTestWalk(t, clientTestSequence(
		clientTestReply(http.StatusOK, clientTestPage(`{"id":"one"}`, false)),
		clientTestReply(http.StatusOK, clientTestPage("", false)),
	))

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 2, rec.count())
}

func TestPaginationKeepsWalkingPastALastFlag(t *testing.T) {
	// `last` stays false until the walk passes the end, so it never marks the final page and a
	// walker that believed it would drop content.
	lastWithContent := `{"content":[{"id":"one"}],"totalPages":-1,"totalElements":-1,` +
		`"last":true,"size":20,"numberOfElements":1,"first":true,"empty":false}`

	got, rec, err := clientTestWalk(t, clientTestSequence(
		clientTestReply(http.StatusOK, lastWithContent),
		clientTestReply(http.StatusOK, clientTestPage("", true)),
	))

	require.NoError(t, err)
	require.Len(t, got, 1, "a page with content is never dropped")
	require.Equal(t, "one", got[0].ID)
	require.Equal(t, 2, rec.count(), "only an empty page ends the walk")
}

func TestPaginationSendsThePageAndTheClampedSize(t *testing.T) {
	_, rec, err := clientTestWalk(t, clientTestSequence(
		clientTestReply(http.StatusOK, clientTestPage(`{"id":"one"}`, false)),
		clientTestReply(http.StatusOK, clientTestPage("", true)),
	))
	require.NoError(t, err)

	require.Equal(t, "0", rec.at(t, 0).Query.Get("page"))
	require.Equal(t, "20", rec.at(t, 0).Query.Get("size"), "the server clamps size to 20")
	require.Equal(t, "1", rec.at(t, 1).Query.Get("page"))
	require.Equal(t, "20", rec.at(t, 1).Query.Get("size"))
}

func TestPaginationNeverSendsASearchFilter(t *testing.T) {
	_, rec, err := clientTestWalk(t, clientTestReply(http.StatusOK, clientTestPage("", true)))
	require.NoError(t, err)

	query := rec.only(t).Query
	require.NotContains(t, query, "searchFilter", "the server accepts it and ignores it")
}

func TestPaginationFailsPastTheThousandPageCap(t *testing.T) {
	_, rec, err := clientTestWalk(t, clientTestReply(http.StatusOK, clientTestPage(`{"id":"one"}`, false)))

	require.Error(t, err)
	require.Contains(t, err.Error(), "1000")
	require.Equal(t, 1000, rec.count(), "the walk must stop at the cap, not run forever")
	requireSentence(t, err.Error())
}

func TestPaginationReturnsTheAPIError(t *testing.T) {
	_, rec, err := clientTestWalk(t, clientTestSequence(
		clientTestReply(http.StatusOK, clientTestPage(`{"id":"one"}`, false)),
		clientTestReply(http.StatusNotFound, clientTestNotFoundBody),
	))

	require.Error(t, err)
	require.True(t, IsGone(err))
	require.Equal(t, 2, rec.count())
}

func TestPaginationCarriesTheCallerQuery(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestPage("", true)))

	query := map[string][]string{"inboxId": {clientTestInboxID}}
	_, err := walkPages[InboxDto](t.Context(), c, "getRulesets", clientTestPagePath, query)
	require.NoError(t, err)

	require.Equal(t, clientTestInboxID, rec.only(t).Query.Get("inboxId"))
}
