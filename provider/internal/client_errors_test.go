package internal

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestNotFoundBody = `{
  "message": "Inbox ID 0690943a does not exist for user 8ce48194",
  "messageLabel": null,
  "caseNumber": "CASE-NF-c83eb905-f733-42ee-8dda-ba8363a2f7c9",
  "errorClass": "Error404NotFound",
  "errorCode": "W_404_ENTITY_NOT_FOUND",
  "retryable": null,
  "retryAfterSeconds": null,
  "details": null,
  "userId": "8ce48194-0000-0000-0000-000000000000",
  "path": "/inboxes/0690943a",
  "method": "GET",
  "operation": "getInbox"
}`

const clientTestUnroutedBody = `{
  "message": "No resource found",
  "caseNumber": "CASE-NF-534ab7c6-bfb9-4cbe-ad72-2a0f8efd0e4f",
  "errorClass": "NoResourceFoundException",
  "errorCode": "E_001_UNEXPECTED_ERROR"
}`

func clientTestGetInboxError(t *testing.T, status int, body string) error {
	t.Helper()
	c, _ := clientTestServer(t, clientTestReply(status, body))

	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.Error(t, err)
	return err
}

func TestAPIErrorIsGoneOnlyForTheEntityNotFoundCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{
			name: "404 with the entity code",
			err:  &APIError{StatusCode: http.StatusNotFound, ErrorCode: "W_404_ENTITY_NOT_FOUND"},
			want: true,
		},
		{
			name: "404 from an unrouted path",
			err:  &APIError{StatusCode: http.StatusNotFound, ErrorCode: "E_001_UNEXPECTED_ERROR"},
			want: false,
		},
		{
			name: "404 with no code",
			err:  &APIError{StatusCode: http.StatusNotFound},
			want: false,
		},
		{
			name: "the entity code on another status",
			err:  &APIError{StatusCode: http.StatusConflict, ErrorCode: "W_404_ENTITY_NOT_FOUND"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsGone(tt.err))
		})
	}
}

func TestAPIErrorIsGoneReadsThroughAWrappedError(t *testing.T) {
	err := clientTestGetInboxError(t, http.StatusNotFound, clientTestNotFoundBody)
	require.True(t, IsGone(err))

	err = clientTestGetInboxError(t, http.StatusNotFound, clientTestUnroutedBody)
	require.False(t, IsGone(err), "an unrouted path must not read as deleted")
}

func TestAPIErrorCarriesTheEnvelopeAndTheRequest(t *testing.T) {
	err := clientTestGetInboxError(t, http.StatusNotFound, clientTestNotFoundBody)

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	require.Equal(t, "W_404_ENTITY_NOT_FOUND", apiErr.ErrorCode)
	require.Equal(t, "Error404NotFound", apiErr.ErrorClass)
	require.Contains(t, apiErr.Message, "does not exist")
	require.Equal(t, "CASE-NF-c83eb905-f733-42ee-8dda-ba8363a2f7c9", apiErr.CaseNumber)
	require.Nil(t, apiErr.Retryable)
	require.Nil(t, apiErr.RetryAfterSeconds)
	require.Equal(t, "getInbox", apiErr.Operation)
	require.Equal(t, http.MethodGet, apiErr.Method)
	require.Equal(t, "/inboxes/"+clientTestInboxID, apiErr.Path)
}

func TestAPIErrorReadsTheRetryEnvelopeFields(t *testing.T) {
	body := `{"errorCode":"E_001_UNEXPECTED_ERROR","retryable":false,"retryAfterSeconds":7}`
	err := clientTestGetInboxError(t, http.StatusServiceUnavailable, body)

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.NotNil(t, apiErr.Retryable)
	require.False(t, *apiErr.Retryable)
	require.NotNil(t, apiErr.RetryAfterSeconds)
	require.Equal(t, 7, *apiErr.RetryAfterSeconds)
}

func TestAPIErrorMapsUnauthorizedAndForbiddenToOneMessage(t *testing.T) {
	const want = "The MailSlurp API key is not valid. Set `MAILSLURP_API_KEY` and try again."
	// A real 401 body names the auth header and the key parameter, so none of it may travel.
	body := `{"errorClass":"Error401Unauthorized","errorCode":"W_401_UNAUTHORIZED",` +
		`"message":"supply the x-api-key header or the apiKey query parameter",` +
		`"caseNumber":"CASE-AUTH-1"}`

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		err := clientTestGetInboxError(t, status, body)
		require.Equal(t, want+" The support case is CASE-AUTH-1.", err.Error(),
			"status %d must say only this, plus the support case", status)
		require.NotContains(t, err.Error(), "apiKey", "the server body must never travel")
		require.NotContains(t, err.Error(), "W_401_UNAUTHORIZED")
	}
}

func TestAPIErrorMapsTooManyRequestsAfterExhaustion(t *testing.T) {
	const want = "MailSlurp received too many requests. Try again later."

	err := clientTestGetInboxError(t, http.StatusTooManyRequests, `{}`)
	require.Contains(t, err.Error(), want)
}

func TestAPIErrorNamesTheCaseNumberAndTheOperation(t *testing.T) {
	err := clientTestGetInboxError(t, http.StatusNotFound, clientTestNotFoundBody)

	require.Contains(t, err.Error(), "CASE-NF-c83eb905-f733-42ee-8dda-ba8363a2f7c9")
	require.Contains(t, err.Error(), "getInbox")
	require.Contains(t, err.Error(), "W_404_ENTITY_NOT_FOUND")
}

func TestAPIErrorSurvivesABodyThatIsNotTheEnvelope(t *testing.T) {
	err := clientTestGetInboxError(t, http.StatusBadRequest, `<html>gateway said no</html>`)

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Empty(t, apiErr.ErrorCode)
	require.Contains(t, err.Error(), "400")
}

func TestAPIErrorNeverRevealsTheAPIKey(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity,
		http.StatusTooManyRequests, http.StatusInternalServerError,
	}

	for _, status := range statuses {
		err := clientTestGetInboxError(t, status, clientTestNotFoundBody)

		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr), "status %d must return an APIError", status)
		require.Equal(t, status, apiErr.StatusCode)
		require.NotContains(t, err.Error(), clientTestKey, "status %d leaked the key", status)
	}

	t.Run("body that is not json", func(t *testing.T) {
		err := clientTestGetInboxError(t, http.StatusBadRequest, `not json at all`)

		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr))
		require.NotContains(t, err.Error(), clientTestKey)
	})

}
