package internal

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// clientTestBlankIDs are the identifiers that must never reach the API: an empty one makes the
// path address the whole collection, and whitespace is what an unset configuration property holds.
var clientTestBlankIDs = []string{"", " ", "  ", "\t\n"}

// The API takes one identifier in a query string, and one operation carries an identifier in both
// the path and the query.
const (
	clientTestQueryIDParameter = "inboxId"
	clientTestUpdateWebhookOp  = "updateWebhook"
)

// clientTestIDCall is one client method that puts an identifier into the request path.
type clientTestIDCall struct {
	operation string
	invoke    func(t *testing.T, c *client, id string) error
}

func clientTestIDCalls() []clientTestIDCall {
	return []clientTestIDCall{
		{"getInbox", func(t *testing.T, c *client, id string) error {
			_, err := c.GetInbox(t.Context(), id)
			return err
		}},
		{"updateInbox", func(t *testing.T, c *client, id string) error {
			_, err := c.UpdateInbox(t.Context(), id, UpdateInboxOptions{})
			return err
		}},
		{"deleteInbox", func(t *testing.T, c *client, id string) error {
			return c.DeleteInbox(t.Context(), id)
		}},
		{"createWebhook", func(t *testing.T, c *client, id string) error {
			_, err := c.CreateInboxWebhook(t.Context(), id, WebhookOptions{URL: "https://example.com"})
			return err
		}},
		{"getWebhook", func(t *testing.T, c *client, id string) error {
			_, err := c.GetWebhook(t.Context(), id)
			return err
		}},
		{clientTestUpdateWebhookOp, func(t *testing.T, c *client, id string) error {
			_, err := c.UpdateWebhook(t.Context(), id, nil, WebhookOptions{URL: "https://example.com"})
			return err
		}},
		{"updateWebhookHeaders", func(t *testing.T, c *client, id string) error {
			_, err := c.UpdateWebhookHeaders(t.Context(), id, WebhookHeaders{})
			return err
		}},
		{"deleteWebhookById", func(t *testing.T, c *client, id string) error {
			return c.DeleteWebhook(t.Context(), id)
		}},
		{"getTemplate", func(t *testing.T, c *client, id string) error {
			_, err := c.GetTemplate(t.Context(), id)
			return err
		}},
		{"updateTemplate", func(t *testing.T, c *client, id string) error {
			_, err := c.UpdateTemplate(t.Context(), id, TemplateOptions{})
			return err
		}},
		{"deleteTemplate", func(t *testing.T, c *client, id string) error {
			return c.DeleteTemplate(t.Context(), id)
		}},
		{"getRuleset", func(t *testing.T, c *client, id string) error {
			_, err := c.GetRuleset(t.Context(), id)
			return err
		}},
		{"deleteRuleset", func(t *testing.T, c *client, id string) error {
			return c.DeleteRuleset(t.Context(), id)
		}},
		{"getInboxForwarder", func(t *testing.T, c *client, id string) error {
			_, err := c.GetForwarder(t.Context(), id)
			return err
		}},
		{"updateInboxForwarder", func(t *testing.T, c *client, id string) error {
			_, err := c.UpdateForwarder(t.Context(), id, ForwarderOptions{})
			return err
		}},
		{"deleteInboxForwarder", func(t *testing.T, c *client, id string) error {
			return c.DeleteForwarder(t.Context(), id)
		}},
		{"getDomain", func(t *testing.T, c *client, id string) error {
			_, err := c.GetDomain(t.Context(), id, false)
			return err
		}},
	}
}

// clientTestForbiddenServer answers every request with a success, and fails the test for sending it.
func clientTestForbiddenServer(t *testing.T) (*client, *clientTestRecorder) {
	t.Helper()
	return clientTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the client sent %s %s: a blank identifier must never reach the API",
			r.Method, r.RequestURI)
		w.Header().Set("Content-Type", clientTestJSONType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"`+clientTestInboxID+`"}`)
	})
}

func TestClientRefusesABlankIdentifierBeforeItSendsAnything(t *testing.T) {
	for _, call := range clientTestIDCalls() {
		for _, id := range clientTestBlankIDs {
			t.Run(call.operation+"/"+strconv.Quote(id), func(t *testing.T) {
				c, rec := clientTestForbiddenServer(t)

				err := call.invoke(t, c, id)

				require.Error(t, err, "a blank identifier must fail the call")
				require.Equal(t, 0, rec.count(), "a blank identifier must send no request")
				require.Contains(t, err.Error(), call.operation, "the error must name the operation")
				require.False(t, IsGone(err), "the refusal must never read as an entity that is gone")
			})
		}
	}
}

// clientTestQueryIDCall is one client method that puts an identifier into the query string.
type clientTestQueryIDCall struct {
	operation string
	invoke    func(t *testing.T, c *client, id string) error
}

func clientTestQueryIDCalls() []clientTestQueryIDCall {
	return []clientTestQueryIDCall{
		{"createNewRuleset", func(t *testing.T, c *client, id string) error {
			_, err := c.CreateRuleset(t.Context(), id, RulesetOptions{
				Scope: "RECEIVING_EMAILS", Action: "BLOCK", Target: "*@spam.example.com",
			})
			return err
		}},
		{"createNewInboxForwarder", func(t *testing.T, c *client, id string) error {
			_, err := c.CreateForwarder(t.Context(), id, clientTestForwarderOptions())
			return err
		}},
		{clientTestUpdateWebhookOp, func(t *testing.T, c *client, id string) error {
			_, err := c.UpdateWebhook(t.Context(), clientTestWebhookID, &id, clientTestWebhookOptions())
			return err
		}},
	}
}

// TestClientRefusesABlankIdentifierInTheQueryBeforeItSendsAnything covers the carrier the path
// guard misses. The spec marks inboxId optional, so absence is legal and a blank value is not.
func TestClientRefusesABlankIdentifierInTheQueryBeforeItSendsAnything(t *testing.T) {
	for _, call := range clientTestQueryIDCalls() {
		for _, id := range clientTestBlankIDs {
			t.Run(call.operation+"/"+strconv.Quote(id), func(t *testing.T) {
				c, rec := clientTestForbiddenServer(t)

				err := call.invoke(t, c, id)

				require.Error(t, err, "a blank identifier must fail the call")
				require.Equal(t, 0, rec.count(), "a blank identifier must send no request")
				require.Contains(t, err.Error(), call.operation, "the error must name the operation")
				require.Contains(t, err.Error(), clientTestQueryIDParameter,
					"the error must name the parameter")
				require.False(t, IsGone(err), "the refusal must never read as an entity that is gone")
			})
		}
	}
}

// clientTestQuerySetID matches a query parameter that carries an identifier. Every identifier the
// API takes in the query string has a name that ends in Id.
var clientTestQuerySetID = regexp.MustCompile(`query\.Set\("[A-Za-z]+Id"`)

// TestClientGuardsEveryIdentifierInTheQuery pins the query table to the client source, so a new
// method that puts an identifier into the query string cannot land without a case here.
func TestClientGuardsEveryIdentifierInTheQuery(t *testing.T) {
	sites := 0
	for _, source := range clientTestSourceFiles(t) {
		raw, err := os.ReadFile(source)
		require.NoError(t, err)
		sites += len(clientTestQuerySetID.FindAllString(string(raw), -1))
	}

	require.Equal(t, sites, len(clientTestQueryIDCalls()),
		"every query identifier call site needs one blank identifier case")
}

// TestClientRefusesAPathThatNamesOnlyACollection pins the guard to the shapes where a blank id
// collapses the path to a collection-wide endpoint, whatever caller or builder produced it.
func TestClientRefusesAPathThatNamesOnlyACollection(t *testing.T) {
	refused := []string{"", "/", "/webhooks/", "/inboxes/", "/inboxes//webhooks",
		"/webhooks/%20", "/webhooks/%09%0A/headers", "//webhooks"}
	for _, path := range refused {
		require.True(t, hasBlankPathSegment(path), "%q collapses onto a collection", path)
	}

	allowed := []string{"/webhooks", "/inboxes/withOptions", "/rulesets",
		"/inboxes/" + clientTestInboxID, "/inboxes/" + clientTestInboxID + "/webhooks",
		"/webhooks/" + clientTestWebhookID + "/headers"}
	for _, path := range allowed {
		require.False(t, hasBlankPathSegment(path), "%q names a resource", path)
	}
}

// clientTestSourceFiles lists the client source files a call-site pin reads, without the tests.
func clientTestSourceFiles(t *testing.T) []string {
	t.Helper()
	globbed, err := filepath.Glob("client*.go")
	require.NoError(t, err)

	sources := []string{}
	for _, source := range globbed {
		if !strings.HasSuffix(source, "_test.go") {
			sources = append(sources, source)
		}
	}
	require.NotEmpty(t, sources)
	return sources
}

// TestClientGuardsEveryIdentifierInThePath pins the table above to the client source, so a new
// method that builds a path from an identifier cannot land without a case here.
func TestClientGuardsEveryIdentifierInThePath(t *testing.T) {
	sites := 0
	for _, source := range clientTestSourceFiles(t) {
		raw, err := os.ReadFile(source)
		require.NoError(t, err)
		sites += strings.Count(string(raw), "pathOf(") - strings.Count(string(raw), "func pathOf(")
	}

	require.Equal(t, sites, len(clientTestIDCalls()),
		"every pathOf call site needs one blank identifier case")
}

// requireSentence holds one diagnostic to the shape this repository ships. A refusal that the
// client returns with no wrapped cause reaches a Pulumi diagnostic whole, so it reads as a sentence.
func requireSentence(t *testing.T, message string) {
	t.Helper()
	require.NotEmpty(t, message, "a diagnostic must carry text")
	opening := message[:1]
	require.Equal(t, strings.ToUpper(opening), opening,
		"the diagnostic must open with a capital letter: %q", message)
	require.True(t, strings.HasSuffix(message, "."),
		"the diagnostic must close with a full stop: %q", message)
}

// The two blank-identifier refusals are the diagnostics a program meets most, because an unset
// property reaches them before any request goes out.
func TestTheBlankIdentifierRefusalsReadAsSentences(t *testing.T) {
	c, _ := clientTestForbiddenServer(t)

	pathErr := c.DeleteInbox(t.Context(), " ")
	require.Error(t, pathErr)
	requireSentence(t, pathErr.Error())

	_, queryErr := c.CreateRuleset(t.Context(), " ", RulesetOptions{
		Scope:  string(RulesetScopeReceivingEmails),
		Action: string(RulesetActionBlock),
		Target: testRulesetTarget,
	})
	require.Error(t, queryErr)
	requireSentence(t, queryErr.Error())
}
