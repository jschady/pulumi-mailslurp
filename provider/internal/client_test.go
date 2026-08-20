package internal

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// A stale committed mock must break this build, not a resource test much further away.
var _ Client = (*MockClient)(nil)

const (
	clientTestKey         = "test-api-key-6f3c9a21b4d7"
	clientTestInboxID     = "0690943a-5b02-41b5-b80a-d486a256215a"
	clientTestWebhookID   = "2d276498-7dc2-41cc-83eb-2b0dde444532"
	clientTestTemplateID  = "3f81a0c7-1d4e-4b2a-9c6f-5e7d8a9b0c1d"
	clientTestRulesetID   = "4a92b1d8-2e5f-4c3b-8d7e-6f8a9b0c1d2e"
	clientTestForwarderID = "5b03c2e9-3f60-4d4c-9e8f-7a9b0c1d2e3f"
	clientTestDomainID    = "6c14d3fa-4071-4e5d-af90-8b0c1d2e3f40"
	clientTestJSONType    = "application/json"
)

type clientTestRequest struct {
	Method     string
	Path       string
	RequestURI string
	Query      url.Values
	Header     http.Header
	Body       []byte
}

// body decodes the recorded request body into a generic map.
func (r clientTestRequest) body(t *testing.T) map[string]any {
	t.Helper()
	decoded := map[string]any{}
	require.NoError(t, json.Unmarshal(r.Body, &decoded))
	return decoded
}

type clientTestRecorder struct {
	mu       sync.Mutex
	requests []clientTestRequest
}

func (r *clientTestRecorder) record(req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(raw))

	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, clientTestRequest{
		Method:     req.Method,
		Path:       req.URL.Path,
		RequestURI: req.RequestURI,
		Query:      req.URL.Query(),
		Header:     req.Header.Clone(),
		Body:       raw,
	})
}

func (r *clientTestRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *clientTestRecorder) at(t *testing.T, i int) clientTestRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Greater(t, len(r.requests), i, "the client must have sent at least %d requests", i+1)
	return r.requests[i]
}

func (r *clientTestRecorder) only(t *testing.T) clientTestRequest {
	t.Helper()
	require.Equal(t, 1, r.count(), "the client must have sent exactly one request")
	return r.at(t, 0)
}

// clientTestServer starts a recording test server and returns a client aimed at it.
func clientTestServer(t *testing.T, handler http.HandlerFunc) (*client, *clientTestRecorder) {
	t.Helper()
	rec := &clientTestRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	c, err := newClient(server.URL, clientTestKey)
	require.NoError(t, err)
	return c, rec
}

// clientTestReply answers every request with one status and one body.
func clientTestReply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", clientTestJSONType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// clientTestSequence answers request N with step N and repeats the last step after that.
func clientTestSequence(steps ...http.HandlerFunc) http.HandlerFunc {
	var seen atomic.Int64
	return func(w http.ResponseWriter, r *http.Request) {
		i := int(seen.Add(1)) - 1
		if i >= len(steps) {
			i = len(steps) - 1
		}
		steps[i](w, r)
	}
}

func clientTestPtr[T any](v T) *T {
	return &v
}

// readClientSource reads one client source file so a test can guard against a wiring that no
// behavioural assertion can reach, such as an endpoint that must never be called.
func readClientSource(name string) (string, error) {
	raw, err := os.ReadFile(name)
	return string(raw), err
}

func TestClientSendsTheAPIKeyHeaderAndNeverAQueryParameter(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, `{"id":"`+clientTestInboxID+`"}`))

	_, err := c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, clientTestKey, got.Header.Get("x-api-key"))
	require.Empty(t, got.Query.Get("apiKey"), "the key must never ride in the query string")
	require.Empty(t, got.Query.Get("accessKey"), "the key must never ride in the query string")
	require.Empty(t, got.Header.Get("Authorization"))
}

func TestClientPinsTheBaseURLWhenNoEndpointIsGiven(t *testing.T) {
	c, err := newClient("", clientTestKey)
	require.NoError(t, err)
	require.Equal(t, "https://api.mailslurp.com", c.baseURL)
}

func TestClientTrimsATrailingSlashFromTheEndpoint(t *testing.T) {
	rec := &clientTestRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		clientTestReply(http.StatusOK, `{"id":"x"}`)(w, r)
	}))
	t.Cleanup(server.Close)

	c, err := newClient(server.URL+"/", clientTestKey)
	require.NoError(t, err)
	_, err = c.GetInbox(t.Context(), clientTestInboxID)
	require.NoError(t, err)

	require.Equal(t, "/inboxes/"+clientTestInboxID, rec.only(t).Path)
}

func TestClientRejectsAnEndpointThatIsNotAURL(t *testing.T) {
	_, err := newClient("http://a b c", clientTestKey)
	require.Error(t, err)
}

// The absences are load-bearing: a list method answers a null inboxType that loops the diff, and
// `PATCH /rulesets` and `PUT /rulesets` are evaluators that a lifecycle method must never reach.
func TestClientInterfaceCarriesTheV1SurfaceOnly(t *testing.T) {
	want := []string{
		"CreateAccountWebhook", "CreateForwarder", "CreateInbox", "CreateInboxWebhook",
		"CreateRuleset", "CreateTemplate", "DeleteForwarder", "DeleteInbox", "DeleteRuleset",
		"DeleteTemplate", "DeleteWebhook", "GetDomain", "GetForwarder", "GetInbox", "GetRuleset",
		"GetTemplate", "GetWebhook", "UpdateForwarder", "UpdateInbox", "UpdateTemplate",
		"UpdateWebhook", "UpdateWebhookHeaders",
	}

	iface := reflect.TypeOf((*Client)(nil)).Elem()
	got := make([]string, 0, iface.NumMethod())
	for i := range iface.NumMethod() {
		got = append(got, iface.Method(i).Name)
	}

	require.Equal(t, want, got, "the mock boundary must stay at the 22 v1 methods")
}

func TestClientSourceNeverNamesTheSearchFilterParameter(t *testing.T) {
	sources, err := filepath.Glob("client*.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources)

	checked := 0
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(source)
		require.NoError(t, err)
		require.NotContains(t, strings.ToLower(string(raw)), "searchfilter",
			"%s must not wire searchFilter: the server accepts it and ignores it", source)
		checked++
	}
	require.Equal(t, 11, checked, "the client is 11 source files")
}
