package examples

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The reader this package uses to watch the account. The provider tests keep their own copy of
// these calls, because Go refuses an import of a directory named internal from outside its tree.

const (
	//nolint:gosec // G101: this names the environment variable, and holds no key.
	apiKeyVariable = "MAILSLURP_API_KEY"

	baseURL      = "https://api.mailslurp.com"
	apiKeyHeader = "x-api-key"
	callTimeout  = 60 * time.Second

	testNamePrefix = "pulumi-test-"

	// listWindow and listPageSize match the sweeper of the provider tests, so an object this
	// package leaves behind is one that sweeper still reaches.
	listWindow   = 7 * 24 * time.Hour
	listPageSize = 20
)

// The object kinds this package names. Each name carries its kind, which is what the sweeper of
// the provider tests matches on.
const (
	inboxKind     = "inbox"
	webhookKind   = "webhook"
	rulesetKind   = "ruleset"
	templateKind  = "template"
	forwarderKind = "forwarder"
)

// The classes of the account, which are the plurals of the kinds plus the domains this account
// does not create.
const (
	domainClass    = "domains"
	inboxClass     = "inboxes"
	webhookClass   = "webhooks"
	rulesetClass   = "rulesets"
	templateClass  = "templates"
	forwarderClass = "forwarders"
)

// newTestName builds a collision-free object name. crypto/rand.Read fills the buffer or panics
// inside the runtime, so it reports no error worth handling.
func newTestName(kind string) string {
	var raw [4]byte
	_, _ = rand.Read(raw[:])
	return testNamePrefix + kind + "-" + hex.EncodeToString(raw[:])
}

// A ruleset and a forwarder carry no name of their own, so the run marker rides the one property
// each of them has. Both hosts belong to the reserved documentation domain.
func rulesetTargetFor(name string) string      { return "*@" + name + ".example.com" }
func forwarderRecipientFor(name string) string { return name + "@example.com" }

func apiKey() string { return os.Getenv(apiKeyVariable) }

// requireAPIKey skips the calling test when the credential is absent, and names the variable.
func requireAPIKey(t *testing.T) string {
	t.Helper()
	key := apiKey()
	if key == "" {
		t.Skipf("set %s to run the example programs", apiKeyVariable)
	}
	return key
}

// apiCall sends one request to the MailSlurp API and answers the status and the body.
func apiCall(ctx context.Context, key, method, path string, query url.Values, body []byte,
) (int, []byte, error) {
	address := baseURL + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		payload = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, payload)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set(apiKeyHeader, key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: callTimeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	answered, err := readAll(resp)
	return resp.StatusCode, answered, err
}

func readAll(resp *http.Response) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// objectClass is one class of account object: where it is listed, how its entries are named, and
// where one entry is deleted.
type objectClass struct {
	name string
	path string
	// query answers the list query. The window and the page bound belong to the class.
	query func() url.Values
	// paged reports whether the endpoint answers a page object rather than a bare array.
	paged bool
	// marker answers the run marker one entry carries, or the empty string when it carries none.
	marker func(objectEntry) string
	// remove answers the path that deletes one entry.
	remove func(id string) string
}

// objectEntry is the part of every list projection this package reads.
type objectEntry struct {
	ID                  string   `json:"id"`
	Name                *string  `json:"name"`
	Target              string   `json:"target"`
	ForwardToRecipients []string `json:"forwardToRecipients"`
}

func namedMarker(entry objectEntry) string {
	if entry.Name == nil {
		return ""
	}
	return *entry.Name
}

func pagedQuery(extra map[string]string) func() url.Values {
	return func() url.Values {
		query := url.Values{}
		query.Set("page", "0")
		query.Set("size", strconv.Itoa(listPageSize))
		query.Set("sort", "DESC")
		for key, value := range extra {
			query.Set(key, value)
		}
		return query
	}
}

// sinceParameter bounds a list to the objects a run of this suite can have created.
const sinceParameter = "since"

func sinceTheWindow() string {
	return time.Now().UTC().Add(-listWindow).Format(time.RFC3339)
}

// accountClasses is every object class this account holds. A class missing here would leave a leak
// of that class invisible to the end-state check.
func accountClasses() []objectClass {
	return []objectClass{
		{
			name: domainClass, path: "/domains", query: func() url.Values { return nil },
			marker: func(objectEntry) string { return "" },
			remove: func(string) string { return "" },
		},
		{
			name: inboxClass, path: "/inboxes/paginated", paged: true,
			query:  pagedQuery(map[string]string{sinceParameter: sinceTheWindow()}),
			marker: namedMarker,
			remove: func(id string) string { return "/inboxes/" + id },
		},
		{
			name: webhookClass, path: "/webhooks/paginated", paged: true,
			query:  pagedQuery(map[string]string{"includeAccountWide": "true"}),
			marker: namedMarker,
			remove: func(id string) string { return "/webhooks/" + id },
		},
		{
			name: rulesetClass, path: "/rulesets", paged: true, query: pagedQuery(nil),
			marker: func(entry objectEntry) string { return entry.Target },
			remove: func(id string) string { return "/rulesets/" + id },
		},
		{
			name: templateClass, path: "/templates/paginated", paged: true,
			query:  pagedQuery(map[string]string{sinceParameter: sinceTheWindow()}),
			marker: namedMarker,
			remove: func(id string) string { return "/templates/" + id },
		},
		{
			name: forwarderClass, path: "/forwarders", paged: true,
			query: pagedQuery(map[string]string{sinceParameter: sinceTheWindow()}),
			marker: func(entry objectEntry) string {
				if len(entry.ForwardToRecipients) != 1 {
					return ""
				}
				return entry.ForwardToRecipients[0]
			},
			remove: func(id string) string { return "/forwarders/" + id },
		},
	}
}

// list reads one page of one class.
func (class objectClass) list(ctx context.Context, key string) ([]objectEntry, error) {
	status, body, err := apiCall(ctx, key, http.MethodGet, class.path, class.query(), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("the %s list answered the status %d", class.name, status)
	}
	if !class.paged {
		var entries []objectEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			return nil, err
		}
		return entries, nil
	}
	var page struct {
		Content []objectEntry `json:"content"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, err
	}
	return page.Content, nil
}

// sweepMarked deletes every entry of one class that carries exactly this marker, and answers how
// many it removed. A blank identifier reaches the whole collection, so it is never deleted.
func (class objectClass) sweepMarked(ctx context.Context, key, marker string) int {
	if marker == "" {
		return 0
	}
	entries, err := class.list(ctx, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "the sweep could not list the %s: %v\n", class.name, err)
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if entry.ID == "" || class.marker(entry) != marker {
			continue
		}
		path := class.remove(entry.ID)
		if path == "" {
			continue
		}
		status, _, err := apiCall(ctx, key, http.MethodDelete, path, nil, nil)
		if err != nil || (status != http.StatusOK && status != http.StatusNoContent &&
			status != http.StatusNotFound) {
			fmt.Fprintf(os.Stderr, "the sweep could not delete a %s: %v %d\n", class.name, err, status)
			continue
		}
		removed++
	}
	return removed
}

// readAccount counts every class the account holds. One call reads all six, so a reader that
// answered nothing at all is a failed run rather than an account that looks clean.
func readAccount(ctx context.Context, key string) (map[string]int, error) {
	counted := map[string]int{}
	for _, class := range accountClasses() {
		entries, err := class.list(ctx, key)
		if err != nil {
			return nil, err
		}
		counted[class.name] = len(entries)
	}
	return counted, nil
}

func surveyLine(label string, counted map[string]int) string {
	return fmt.Sprintf(
		"account survey %s: domains=%d inboxes=%d webhooks=%d rulesets=%d templates=%d forwarders=%d",
		label, counted[domainClass], counted[inboxClass], counted[webhookClass],
		counted[rulesetClass], counted[templateClass], counted[forwarderClass])
}

// surveyTheAccount reads the account and writes the counts to the log of the calling leg, which is
// what the evidence of a run carries.
func surveyTheAccount(t *testing.T, key, label string) map[string]int {
	t.Helper()
	counted, err := readAccount(context.Background(), key)
	require.NoError(t, err, "every list of the account should be readable")
	t.Log(surveyLine(label, counted))
	return counted
}
