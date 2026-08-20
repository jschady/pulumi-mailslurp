package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const clientTestInboxBody = `{"id":"` + clientTestInboxID + `","emailAddress":"a@mailslurp.net"}`

// clientTestDropConnection answers by closing the connection, which the caller sees as a
// transport error rather than an HTTP status.
func clientTestDropConnection(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}

func TestRetrySucceedsAfterOneTooManyRequests(t *testing.T) {
	c, rec := clientTestServer(t, clientTestSequence(
		clientTestReply(http.StatusTooManyRequests, `{"errorCode":"E_001_UNEXPECTED_ERROR"}`),
		clientTestReply(http.StatusOK, clientTestInboxBody),
	))

	inbox, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)
	require.Equal(t, clientTestInboxID, inbox.ID)
	require.Equal(t, 2, rec.count())
}

func TestRetrySucceedsAfterThreeServerErrors(t *testing.T) {
	c, rec := clientTestServer(t, clientTestSequence(
		clientTestReply(http.StatusInternalServerError, `{}`),
		clientTestReply(http.StatusBadGateway, `{}`),
		clientTestReply(http.StatusServiceUnavailable, `{}`),
		clientTestReply(http.StatusOK, clientTestInboxBody),
	))

	inbox, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)
	require.Equal(t, clientTestInboxID, inbox.ID)
	require.Equal(t, 4, rec.count())
}

func TestRetryStopsAtFourAttempts(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusInternalServerError, `{}`))

	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.Error(t, err)
	require.Equal(t, 4, rec.count(), "4 attempts total: 1 initial and 3 retries")
}

func TestRetryNeverRetriesAClientError(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity,
	}

	for _, status := range statuses {
		c, rec := clientTestServer(t, clientTestReply(status, `{"errorCode":"W_TEST"}`))

		_, err := c.GetInbox(t.Context(), clientTestInboxID)
		require.Error(t, err, "status %d must fail", status)
		require.Equal(t, 1, rec.count(), "status %d must not be retried", status)
	}
}

func TestRetryHonoursTheRetryAfterHeaderAndBoundsTheWait(t *testing.T) {
	c, rec := clientTestServer(t, clientTestSequence(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "2")
			clientTestReply(http.StatusTooManyRequests, `{}`)(w, r)
		},
		clientTestReply(http.StatusOK, clientTestInboxBody),
	))

	start := time.Now()
	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Equal(t, 2, rec.count())
	require.GreaterOrEqual(t, elapsed, 2*time.Second, "the server asked for 2 seconds")
	require.Less(t, elapsed, 4*time.Second, "2 seconds bounds the wait")
}

func TestRetryComputesTheServerDirectedWait(t *testing.T) {
	five := 5
	tests := []struct {
		name      string
		header    string
		envelope  *int
		want      time.Duration
		wantFound bool
	}{
		{name: "no signal", wantFound: false},
		{name: "header seconds", header: "2", want: 2 * time.Second, wantFound: true},
		{name: "header over the cap", header: "600", want: 30 * time.Second, wantFound: true},
		{name: "header that is not a number", header: "soon", wantFound: false},
		{name: "envelope seconds", envelope: &five, want: 5 * time.Second, wantFound: true},
		{name: "header wins over envelope", header: "2", envelope: &five, want: 2 * time.Second, wantFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := serverWait(tt.header, tt.envelope)
			require.Equal(t, tt.wantFound, found)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRetryReadsARetryAfterHTTPDate(t *testing.T) {
	header := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)

	got, found := serverWait(header, nil)
	require.True(t, found)
	require.Greater(t, got, 1*time.Second)
	require.LessOrEqual(t, got, 3*time.Second)
}

func TestRetryClampsAPastRetryAfterDateToZero(t *testing.T) {
	header := time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat)

	got, found := serverWait(header, nil)
	require.True(t, found)
	require.Equal(t, time.Duration(0), got)
}

func TestRetryBackoffDoublesAndCapsAtEightSeconds(t *testing.T) {
	require.Equal(t, 250*time.Millisecond, backoffWait(1))
	require.Equal(t, 500*time.Millisecond, backoffWait(2))
	require.Equal(t, 1000*time.Millisecond, backoffWait(3))
	require.Equal(t, 8*time.Second, backoffWait(9))
}

func TestRetryAppliesFullJitter(t *testing.T) {
	const draws = 200
	base := time.Second

	sawShorter, sawPositive := false, false
	for range draws {
		got := fullJitter(base)
		require.GreaterOrEqual(t, got, time.Duration(0))
		require.LessOrEqual(t, got, base)
		if got < base {
			sawShorter = true
		}
		if got > 0 {
			sawPositive = true
		}
	}

	require.True(t, sawShorter, "full jitter must sometimes wait less than the computed value")
	require.True(t, sawPositive, "full jitter must not collapse to zero")
}

func TestRetryTreatsAnEnvelopeRetryableFalseAsTerminal(t *testing.T) {
	body := `{"errorCode":"E_001_UNEXPECTED_ERROR","retryable":false,"retryAfterSeconds":null}`
	c, rec := clientTestServer(t, clientTestReply(http.StatusServiceUnavailable, body))

	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.Error(t, err)
	require.Equal(t, 1, rec.count(), "retryable false is terminal whatever the status")
}

func TestRetryReturnsTheContextErrorMidBackoff(t *testing.T) {
	c, rec := clientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		clientTestReply(http.StatusServiceUnavailable, `{}`)(w, r)
	})

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.GetInbox(ctx, clientTestInboxID)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, rec.count())
	require.Less(t, elapsed, 5*time.Second, "the wait must stop when the context ends")
}

func TestRetryReplaysTheIdenticalBodyOnARetry(t *testing.T) {
	c, rec := clientTestServer(t, clientTestSequence(
		clientTestReply(http.StatusInternalServerError, `{}`),
		clientTestReply(http.StatusCreated, clientTestInboxBody),
	))

	opts := CreateInboxOptions{
		Name:        clientTestPtr("retry-body"),
		Description: clientTestPtr("the body must survive a retry"),
		Tags:        []string{"a", "b"},
	}
	_, err := c.CreateInbox(t.Context(), opts)
	require.NoError(t, err)
	require.Equal(t, 2, rec.count())

	first, second := rec.at(t, 0), rec.at(t, 1)
	require.NotEmpty(t, first.Body)
	require.Equal(t, first.Body, second.Body, "a retried POST must replay the identical body")

	var decoded CreateInboxOptions
	require.NoError(t, json.Unmarshal(second.Body, &decoded))
	require.Equal(t, opts, decoded)
}

func TestRetryRetriesATransportError(t *testing.T) {
	c, rec := clientTestServer(t, clientTestSequence(
		clientTestDropConnection,
		clientTestReply(http.StatusOK, clientTestInboxBody),
	))

	inbox, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)
	require.Equal(t, clientTestInboxID, inbox.ID)
	require.Equal(t, 2, rec.count())
}

func TestRetryGivesUpOnAnUnreachableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()

	c, err := newClient(endpoint, clientTestKey)
	require.NoError(t, err)

	_, err = c.GetInbox(t.Context(), clientTestInboxID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "getInbox")
	require.NotContains(t, err.Error(), clientTestKey)
}
