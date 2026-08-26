package replaytest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// callTimeout bounds one request. It matches the timeout the provider client carries.
const callTimeout = 60 * time.Second

// missingCassetteMessage names the file and the targets that write it.
const missingCassetteMessage = "The cassette %s is missing. Run %s to write it."

// unusedInteractionMessage names the call the cassette holds and the test never made.
const unusedInteractionMessage = "The cassette holds %s %s, and the test never sent it."

// CassetteName turns a test name into a cassette name. A subtest name carries a slash, which a
// file name cannot hold.
func CassetteName(testName string) string {
	return strings.ReplaceAll(testName, "/", "_")
}

// ClientFor wraps one transport in an HTTP client.
func ClientFor(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport, Timeout: callTimeout}
}

// Fixture holds the recorder and the seed of one test.
type Fixture struct {
	mode Mode
	rec  *recorder.Recorder
	seed *Source
	log  *requestLog
}

// New builds the fixture of one test, and stops the recorder when the test ends.
func New(t *testing.T, cassetteDir string) *Fixture {
	t.Helper()
	mode := ModeOf(t)

	fixture, err := newFixture(mode, cassetteDir, CassetteName(t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.Stop(); err != nil {
			t.Errorf("the recorder of %s failed: %v", t.Name(), err)
		}
	})
	return fixture
}

// NewFor builds the fixture of a cassette that belongs to a suite rather than to one test. The
// caller names the cassette, names the transport a record run sends through, and calls Stop when
// the suite ends. A test uses New instead: a test knows its own name, and New stops the recorder
// for it. The suite of the integration tests records the inbox its tests share this way.
func NewFor(mode Mode, cassetteDir, name string, vendor http.RoundTripper) (*Fixture, error) {
	return newFixtureThrough(mode, cassetteDir, CassetteName(name), vendor)
}

// newFixture builds the recorder and the seed source for one cassette name. A record run sends
// through the default transport, which is the transport the provider client reaches.
func newFixture(mode Mode, cassetteDir, name string) (*Fixture, error) {
	return newFixtureThrough(mode, cassetteDir, name, http.DefaultTransport)
}

// newFixtureThrough builds the recorder and the seed source, and sends a record run through the
// transport the caller named.
func newFixtureThrough(mode Mode, cassetteDir, name string, vendor http.RoundTripper) (*Fixture, error) {
	path := filepath.Join(cassetteDir, name)

	rec, err := recorder.New(path,
		recorder.WithMode(recorderMode(mode)),
		recorder.WithRealTransport(vendor),
		recorder.WithSkipRequestLatency(true),
		recorder.WithMatcher(matchRequest),
		recorder.WithHook(scrub, recorder.BeforeSaveHook),
		recorder.WithHook(unusedInteraction(mode), recorder.OnRecorderStopHook),
	)
	if err != nil {
		if errors.Is(err, cassette.ErrCassetteNotFound) {
			//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
			return nil, fmt.Errorf(missingCassetteMessage, path+".yaml", recordTargets)
		}
		return nil, err
	}

	seed, err := sourceFor(mode, cassetteDir, name)
	if err != nil {
		return nil, err
	}

	return &Fixture{mode: mode, rec: rec, seed: seed, log: &requestLog{next: rec}}, nil
}

// Mode answers the mode this fixture runs in.
func (f *Fixture) Mode() Mode { return f.mode }

// Seed answers the byte source the names of this test come from.
func (f *Fixture) Seed() *Source { return f.seed }

// Recorder answers the recorder that reads or writes the cassette.
func (f *Fixture) Recorder() *recorder.Recorder { return f.rec }

// Transport answers the round tripper every call of this test must go through.
func (f *Fixture) Transport() http.RoundTripper { return f.log }

// Client answers an HTTP client that sends every call through the recorder.
func (f *Fixture) Client() *http.Client { return ClientFor(f.log) }

// Requests answers the method and the URL of every request the recorder saw.
func (f *Fixture) Requests() []string { return f.log.snapshot() }

// Stop closes the recorder. Record writes the cassette, and replay reports an interaction the
// test never sent.
func (f *Fixture) Stop() error { return f.rec.Stop() }

// recorderMode maps a mode of this package onto a mode of the recorder.
func recorderMode(mode Mode) recorder.Mode {
	switch mode {
	case Record:
		return recorder.ModeRecordOnly
	case Live:
		return recorder.ModePassthrough
	default:
		return recorder.ModeReplayOnly
	}
}

// unusedInteraction fails a replay that left a recorded call unused, which means the test no
// longer makes the calls the cassette holds. A recording replays nothing, so it reports nothing.
func unusedInteraction(mode Mode) recorder.HookFunc {
	return func(i *cassette.Interaction) error {
		if mode != Replay || i.WasReplayed() {
			return nil
		}
		//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
		return fmt.Errorf(unusedInteractionMessage, i.Request.Method, i.Request.URL)
	}
}

// secretRequestHeaders are the request headers that carry a credential.
var secretRequestHeaders = []string{"X-Api-Key", "Authorization", "Cookie"}

// secretResponseHeaders are the response headers that carry a credential.
var secretResponseHeaders = []string{"Set-Cookie"}

// contentLengthHeader names the length of the body it rides with.
const contentLengthHeader = "Content-Length"

// scrub drops every credential header and every secret body value before the cassette reaches the
// disk. It reads the request and the response of every path, because a credential rides a body on
// any endpoint that takes one.
func scrub(i *cassette.Interaction) error {
	dropHeaders(i.Request.Headers, secretRequestHeaders)
	dropHeaders(i.Response.Headers, secretResponseHeaders)
	i.Request.Body, i.Request.ContentLength =
		redactedBody(i.Request.Body, i.Request.ContentLength, i.Request.Headers)
	i.Response.Body, i.Response.ContentLength =
		redactedBody(i.Response.Body, i.Response.ContentLength, i.Response.Headers)
	return nil
}

// redactedBody answers one body with every secret replaced, and the length a reader of that body
// finds. A redaction shortens the body, so a cassette that kept the recorded length would send a
// reader after bytes the file does not hold. A body the scrub left alone keeps the length it was
// recorded with, and a part that carries no length header gains none.
func redactedBody(body string, length int64, headers http.Header) (string, int64) {
	redacted := RedactBody(body)
	if redacted == body {
		return body, length
	}
	length = int64(len(redacted))
	if headers.Get(contentLengthHeader) != "" {
		headers.Set(contentLengthHeader, strconv.FormatInt(length, 10))
	}
	return redacted, length
}

// RedactedValue is what the scrub writes in place of a secret.
const RedactedValue = "REDACTED"

// SecretMembers are the JSON member names whose string value carries a credential. The comparison
// ignores case, so `apikey` and `APIKey` both match `apiKey`.
var SecretMembers = []string{
	"key", "api_key", "apiKey", "awsSecretKey", "aws_secret_key", "secret", "password",
	"token", "authorization", "basicAuth", "basic_auth",
}

// isSecretMember reports whether one JSON member name carries a credential.
func isSecretMember(name string) bool {
	for _, member := range SecretMembers {
		if strings.EqualFold(name, member) {
			return true
		}
	}
	return false
}

// RedactBody answers one body with the string value of every secret member replaced. A body that
// holds no JSON comes back byte for byte, and so does a body that names no secret member, so the
// matcher still compares the bytes the recording holds.
func RedactBody(body string) string {
	if body == "" {
		return body
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return body
	}
	redacted, changed := redactValue(parsed, false)
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(redacted)
	if err != nil {
		return body
	}
	return string(rewritten)
}

// redactValue walks one JSON value and answers it with every secret replaced. The secret flag
// carries into an array, so a member that holds a list of credentials loses every one of them. It
// does not carry into a nested object: each member of that object answers for its own name.
func redactValue(value any, secret bool) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for name, member := range typed {
			next, hit := redactValue(member, isSecretMember(name))
			changed = changed || hit
			typed[name] = next
		}
		return typed, changed
	case []any:
		changed := false
		for index, member := range typed {
			next, hit := redactValue(member, secret)
			changed = changed || hit
			typed[index] = next
		}
		return typed, changed
	case string:
		if secret && typed != RedactedValue {
			return RedactedValue, true
		}
		return typed, false
	default:
		return value, false
	}
}

// dropHeaders deletes each named header. A cassette holds the canonical spelling of a header, so
// the comparison ignores case.
func dropHeaders(headers http.Header, names []string) {
	for header := range headers {
		for _, name := range names {
			if strings.EqualFold(header, name) {
				delete(headers, header)
			}
		}
	}
}

// volatileQueryKeys are the query keys whose value changes on every run. MailSlurp takes the
// current time in `since`, so a replayed request never carries the recorded value.
var volatileQueryKeys = []string{"since"}

// matchRequest matches a live request against a recorded one. It reads the method, the path, the
// query without the volatile keys, and the body of a write. It reads no header, because the
// scrub drops the credential header the live request still carries.
func matchRequest(r *http.Request, recorded cassette.Request) bool {
	if r.Method != recorded.Method {
		return false
	}
	target, err := url.Parse(recorded.URL)
	if err != nil {
		return false
	}
	if r.URL.Path != target.Path {
		return false
	}
	if stableQuery(r.URL) != stableQuery(target) {
		return false
	}
	if !carriesABody(r.Method) {
		return true
	}
	return bodyMatches(r, recorded.Body)
}

// stableQuery answers the query without the keys whose value changes on every run.
func stableQuery(address *url.URL) string {
	query := address.Query()
	for _, key := range volatileQueryKeys {
		query.Del(key)
	}
	return query.Encode()
}

// carriesABody reports whether the method sends a body the matcher must compare.
func carriesABody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// bodyMatches reads the request body and puts it back, so the round trip still sends it. The live
// body carries the credentials the scrub took out of the cassette, so it is redacted the same way
// before the comparison.
func bodyMatches(r *http.Request, recorded string) bool {
	if r.Body == nil {
		return recorded == ""
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return RedactBody(string(raw)) == RedactBody(recorded)
}

// requestLog keeps the method and the URL of every request, and passes each one on.
type requestLog struct {
	next http.RoundTripper
	mu   sync.Mutex
	seen []string
}

func (l *requestLog) RoundTrip(r *http.Request) (*http.Response, error) {
	l.mu.Lock()
	l.seen = append(l.seen, r.Method+" "+r.URL.String())
	l.mu.Unlock()
	return l.next.RoundTrip(r)
}

// snapshot answers a copy of the log, so a reader never races the next request.
func (l *requestLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seen...)
}
