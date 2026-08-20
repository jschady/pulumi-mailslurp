package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type apiRequest struct {
	method    string
	path      string
	operation string
	query     url.Values
	body      any
}

// call sends one request and decodes the response into a fresh T.
func call[T any](ctx context.Context, c *client, req apiRequest) (*T, error) {
	out := new(T)
	if err := c.do(ctx, req, out); err != nil {
		return nil, err
	}
	return out, nil
}

// pathOf joins a collection path and an identifier the caller supplied. A blank identifier leaves
// a blank segment, which do refuses.
func pathOf(collection, id string) string {
	return collection + "/" + url.PathEscape(id)
}

// hasBlankPathSegment reports whether a path segment names nothing. A blank identifier collapses
// the path onto the collection, and the API routes a collection path to the whole collection.
func hasBlankPathSegment(path string) bool {
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		decoded := segment
		if unescaped, err := url.PathUnescape(segment); err == nil {
			decoded = unescaped
		}
		if strings.TrimSpace(decoded) == "" {
			return true
		}
	}
	return false
}

// blankQueryValue names a query parameter that carries nothing. The spec marks each identifier the
// query takes as optional, so a blank value asks for the account instead of one named entity.
func blankQueryValue(query url.Values) (string, bool) {
	for name, values := range query {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return name, true
			}
		}
	}
	return "", false
}

// The guard returns these two whole, so they are the diagnostic a program reads.
const (
	blankPathIdentifierMessage = "The MailSlurp `%s` call needs an identifier. " +
		"Set the identifier and try again."
	blankQueryValueMessage = "The MailSlurp `%s` call needs a value for `%s`. " +
		"Set the value and try again."
)

// do sends the request, retries what the retry policy allows, and decodes a success into out.
// A nil out discards the body, which is what a delete wants.
func (c *client) do(ctx context.Context, req apiRequest, out any) error {
	if hasBlankPathSegment(req.path) {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return fmt.Errorf(blankPathIdentifierMessage, req.operation)
	}
	if name, blank := blankQueryValue(req.query); blank {
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return fmt.Errorf(blankQueryValueMessage, req.operation, name)
	}

	var body []byte
	if req.body != nil {
		marshalled, err := json.Marshal(req.body)
		if err != nil {
			return fmt.Errorf("the client failed to build the MailSlurp %s body: %w", req.operation, err)
		}
		body = marshalled
	}

	for attempt := 1; ; attempt++ {
		resp, err := c.send(ctx, req, body)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if attempt == maxAttempts {
				return fmt.Errorf("the MailSlurp %s request failed: %w", req.operation, err)
			}
			if waitErr := waitFor(ctx, fullJitter(backoffWait(attempt))); waitErr != nil {
				return waitErr
			}
			continue
		}

		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return decode(resp, req, out)
		}

		apiErr := newAPIError(resp, req)
		wait := retryWait(attempt, resp, apiErr)
		_ = resp.Body.Close()

		if attempt == maxAttempts || !shouldRetry(apiErr) {
			return apiErr
		}
		if waitErr := waitFor(ctx, wait); waitErr != nil {
			return waitErr
		}
	}
}

// send builds a fresh body reader on every attempt so a retry can replay the same bytes.
func (c *client) send(ctx context.Context, req apiRequest, body []byte) (*http.Response, error) {
	target := c.baseURL + req.path
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, target, reader)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set(apiKeyHeader, c.apiKey)
	if body != nil {
		httpReq.Header.Set("Content-Type", contentTypeJSON)
	}

	return c.http.Do(httpReq)
}

func decode(resp *http.Response, req apiRequest, out any) error {
	defer func() { _ = resp.Body.Close() }()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("the client failed to read the MailSlurp %s response: %w", req.operation, err)
	}
	return nil
}
