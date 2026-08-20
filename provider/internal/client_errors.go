package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	errorCodeEntityNotFound = "W_404_ENTITY_NOT_FOUND"
	errorBodyLimit          = 8 << 10
	errorMessageLimit       = 512

	authFailureMessage = "The MailSlurp API key is not valid. " +
		"Set `MAILSLURP_API_KEY` and try again."
	rateLimitMessage = "MailSlurp received too many requests. Try again later."
)

// APIError is a MailSlurp error envelope plus the request that produced it.
type APIError struct {
	StatusCode        int
	ErrorCode         string
	ErrorClass        string
	Message           string
	CaseNumber        string
	Retryable         *bool
	RetryAfterSeconds *int
	Path              string
	Method            string
	Operation         string
}

// errorEnvelope is the error body the API returns. The OpenAPI spec documents no error at all,
// so every property here comes from a live response.
type errorEnvelope struct {
	Message           string `json:"message"`
	CaseNumber        string `json:"caseNumber"`
	ErrorClass        string `json:"errorClass"`
	ErrorCode         string `json:"errorCode"`
	Retryable         *bool  `json:"retryable"`
	RetryAfterSeconds *int   `json:"retryAfterSeconds"`
}

func (e *APIError) Error() string {
	// A 401 body lists the auth header names, so this path adds nothing the server sent.
	if e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden {
		return authFailureMessage + e.caseSentence()
	}

	var message strings.Builder
	if e.StatusCode == http.StatusTooManyRequests {
		message.WriteString(rateLimitMessage)
	} else {
		fmt.Fprintf(&message, "The MailSlurp %s call failed with status %d.",
			e.Operation, e.StatusCode)
	}
	if e.ErrorCode != "" {
		message.WriteString(" The error code is ")
		message.WriteString(e.ErrorCode)
		message.WriteString(".")
	}
	if e.Message != "" {
		message.WriteString(" ")
		message.WriteString(e.Message)
	}
	message.WriteString(e.caseSentence())

	return message.String()
}

func (e *APIError) caseSentence() string {
	if e.CaseNumber == "" {
		return ""
	}
	return " The support case is " + e.CaseNumber + "."
}

// IsGone reports whether the API says the entity does not exist. An unrouted path answers 404
// too, and reading that as "deleted" would drop a live resource out of the state.
func IsGone(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound && apiErr.ErrorCode == errorCodeEntityNotFound
}

func newAPIError(resp *http.Response, req apiRequest) *APIError {
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Path:       req.path,
		Method:     req.method,
		Operation:  req.operation,
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err != nil || len(raw) == 0 {
		return apiErr
	}

	var envelope errorEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return apiErr
	}

	apiErr.ErrorCode = envelope.ErrorCode
	apiErr.ErrorClass = envelope.ErrorClass
	apiErr.Message = truncate(envelope.Message, errorMessageLimit)
	apiErr.CaseNumber = envelope.CaseNumber
	apiErr.Retryable = envelope.Retryable
	apiErr.RetryAfterSeconds = envelope.RetryAfterSeconds

	return apiErr
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}
