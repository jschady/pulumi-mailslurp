package replaytest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// The fake vendor and the values the proofs send it. The key is a fixed fake, and the account it
// names does not exist.
const (
	fakeKey     = "fake-key-00000000-0000-4000-8000-000000000000"
	fakeInboxID = "0690943a-5b02-41b5-b80a-d486a256215a"
	fakeBody    = `{"name":"pulumi-test-inbox"}`

	recordedSince = "2020-01-01T00:00:00Z"
	replayedSince = "2026-08-25T12:00:00Z"
)

// vendorFake answers every call the proofs make, and hands back a cookie the scrub must drop.
func vendorFake(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=opaque")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"`+fakeInboxID+`"}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// sendCall sends one request with the credential header, and answers the body.
func sendCall(t *testing.T, client *http.Client, method, address, body string) (string, error) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, address, reader)
	require.NoError(t, err)
	req.Header.Set("x-api-key", fakeKey)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	answered, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(answered), nil
}

// sendTheThreeCalls makes a read, a list bounded by `since`, and a write. The `since` value
// differs between the recording and the replay, which is what the matcher must forgive.
func sendTheThreeCalls(t *testing.T, client *http.Client, base, since string) {
	t.Helper()
	read, err := sendCall(t, client, http.MethodGet, base+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err)
	require.Contains(t, read, fakeInboxID)

	list := base + "/inboxes?page=0&since=" + url.QueryEscape(since)
	_, err = sendCall(t, client, http.MethodGet, list, "")
	require.NoError(t, err)

	_, err = sendCall(t, client, http.MethodPost, base+"/inboxes", fakeBody)
	require.NoError(t, err)
}

// The round trip of the whole mechanism: a recording writes a cassette and a seed, the cassette
// carries no credential, and a replay answers the same three calls with the vendor stopped.
func TestTheRecorderWritesACassetteAndAReplayServesItWithTheVendorStopped(t *testing.T) {
	dir := t.TempDir()
	const name = "TestRoundTrip"
	server := vendorFake(t)

	recording, err := newFixture(Record, dir, name)
	require.NoError(t, err)
	recordedName := recording.Seed().Hex(4)
	sendTheThreeCalls(t, recording.Client(), server.URL, recordedSince)
	require.NoError(t, recording.Stop())
	server.Close()

	cassettePath := filepath.Join(dir, name+".yaml")
	require.FileExists(t, cassettePath, "record must write the cassette")
	require.FileExists(t, SeedPath(dir, name), "record must write the seed")

	written, err := os.ReadFile(cassettePath)
	require.NoError(t, err)
	require.Contains(t, string(written), fakeInboxID, "the cassette must hold the recorded answer")
	findings, err := CassetteFindings(dir)
	require.NoError(t, err)
	require.Empty(t, findings, "the cassette must carry no credential:\n%s", written)

	replaying, err := newFixture(Replay, dir, name)
	require.NoError(t, err)
	require.Equal(t, recordedName, replaying.Seed().Hex(4), "replay must draw the recorded name")
	sendTheThreeCalls(t, replaying.Client(), server.URL, replayedSince)
	require.NoError(t, replaying.Stop(), "replay must consume every recorded call")
	require.Len(t, replaying.Requests(), 3, "the log must hold the method and the URL of each call")
	require.Contains(t, replaying.Requests()[0], "GET ")
}

// A call the cassette does not hold must fail the test, not reach the vendor.
func TestACallTheCassetteDoesNotHoldFails(t *testing.T) {
	dir := t.TempDir()
	const name = "TestExtraCall"
	server := vendorFake(t)

	recording, err := newFixture(Record, dir, name)
	require.NoError(t, err)
	_, err = sendCall(t, recording.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err)
	require.NoError(t, recording.Stop())
	server.Close()

	replaying, err := newFixture(Replay, dir, name)
	require.NoError(t, err)
	_, err = sendCall(t, replaying.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err, "the recorded call must replay")

	_, err = sendCall(t, replaying.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.Error(t, err, "a second call must find nothing left")
	require.ErrorIs(t, err, cassette.ErrInteractionNotFound)
	require.NoError(t, replaying.Stop())
}

// A cassette the test outgrew must fail loudly. An interaction nobody sent means the test no
// longer makes the calls the recording holds.
func TestAnInteractionTheTestNeverSentFailsAtStop(t *testing.T) {
	dir := t.TempDir()
	const name = "TestUnusedInteraction"
	server := vendorFake(t)

	recording, err := newFixture(Record, dir, name)
	require.NoError(t, err)
	_, err = sendCall(t, recording.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err)
	_, err = sendCall(t, recording.Client(), http.MethodGet, server.URL+"/webhooks", "")
	require.NoError(t, err)
	require.NoError(t, recording.Stop())
	server.Close()

	replaying, err := newFixture(Replay, dir, name)
	require.NoError(t, err)
	_, err = sendCall(t, replaying.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err)

	err = replaying.Stop()
	require.Error(t, err, "an unused interaction must fail at Stop")
	require.ErrorContains(t, err, "/webhooks", "the refusal must name the call nobody sent")
}

// Live sends every call on and writes no cassette, which is how the acceptance run works today.
func TestLiveSendsTheCallOnAndWritesNoCassette(t *testing.T) {
	dir := t.TempDir()
	const name = "TestLive"
	server := vendorFake(t)

	fixture, err := newFixture(Live, dir, name)
	require.NoError(t, err)
	answered, err := sendCall(t, fixture.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err)
	require.Contains(t, answered, fakeInboxID, "live must reach the vendor")

	require.NoError(t, fixture.Stop())
	require.NoFileExists(t, filepath.Join(dir, name+".yaml"), "live must write no cassette")
}

func TestAMissingCassetteNamesThePathAndTheRecordTargets(t *testing.T) {
	dir := t.TempDir()

	_, err := newFixture(Replay, dir, "TestAbsent")
	require.Error(t, err, "replay with no cassette must fail")
	require.ErrorContains(t, err, filepath.Join(dir, "TestAbsent.yaml"), "the refusal must name the file")
	require.ErrorContains(t, err, "make record_examples")
	require.ErrorContains(t, err, "make record_integration")
}

// New reads the mode from the environment and stops the recorder when the test ends. The check
// below is registered first, so it runs after the cleanup that New adds.
func TestNewStopsTheRecorderWhenTheTestEnds(t *testing.T) {
	dir := t.TempDir()
	server := vendorFake(t)
	cassettePath := filepath.Join(dir, "TestNewStopsTheRecorderWhenTheTestEnds.yaml")
	t.Cleanup(func() {
		if _, err := os.Stat(cassettePath); err != nil {
			t.Errorf("New must stop the recorder, which writes the cassette: %v", err)
		}
	})
	t.Setenv(ModeVariable, string(Record))

	fixture := New(t, dir)
	require.Equal(t, Record, fixture.Mode())
	_, err := sendCall(t, fixture.Client(), http.MethodGet, server.URL+"/inboxes/"+fakeInboxID, "")
	require.NoError(t, err)
}

// liveRequest builds the request a replay sends, which carries the credential header.
func liveRequest(t *testing.T, method, address, body string) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, address, reader)
	require.NoError(t, err)
	req.Header.Set("x-api-key", fakeKey)
	return req
}

const vendorBase = "https://api.mailslurp.com"

// The scrub drops the credential header, so a matcher that read the headers would never match.
func TestTheMatcherIgnoresTheCredentialHeader(t *testing.T) {
	recorded := cassette.Request{
		Method:  http.MethodGet,
		URL:     vendorBase + "/inboxes/" + fakeInboxID,
		Headers: http.Header{},
	}

	require.True(t, matchRequest(liveRequest(t, http.MethodGet, recorded.URL, ""), recorded),
		"a request with the key header must match a cassette without it")
}

// MailSlurp takes the current time in `since`, so the recorded value never comes back.
func TestTheMatcherIgnoresTheSinceQuery(t *testing.T) {
	recorded := cassette.Request{
		Method: http.MethodGet,
		URL:    vendorBase + "/inboxes?page=0&since=" + url.QueryEscape(recordedSince),
	}
	live := vendorBase + "/inboxes?page=0&since=" + url.QueryEscape(replayedSince)

	require.True(t, matchRequest(liveRequest(t, http.MethodGet, live, ""), recorded),
		"another `since` must still match")
}

// The forgiveness stops at `since`: every other part of the request still has to agree.
func TestTheMatcherReadsTheMethodThePathTheQueryAndTheBody(t *testing.T) {
	recorded := cassette.Request{
		Method: http.MethodGet,
		URL:    vendorBase + "/inboxes?page=0&since=" + url.QueryEscape(recordedSince),
	}
	written := cassette.Request{Method: http.MethodPost, URL: vendorBase + "/inboxes", Body: fakeBody}

	require.False(t, matchRequest(liveRequest(t, http.MethodPost, recorded.URL, ""), recorded),
		"another method must not match")
	require.False(t, matchRequest(liveRequest(t, http.MethodGet, vendorBase+"/webhooks?page=0", ""), recorded),
		"another path must not match")
	require.False(t, matchRequest(liveRequest(t, http.MethodGet, vendorBase+"/inboxes?page=1", ""), recorded),
		"another page must not match")
	require.True(t, matchRequest(liveRequest(t, http.MethodPost, written.URL, fakeBody), written),
		"the recorded body must match")
	require.False(t, matchRequest(liveRequest(t, http.MethodPost, written.URL, `{"name":"other"}`), written),
		"another body must not match")
}

// The matcher reads the body, so it must put the body back for the round trip that follows.
func TestTheMatcherLeavesTheBodyReadable(t *testing.T) {
	written := cassette.Request{Method: http.MethodPost, URL: vendorBase + "/inboxes", Body: fakeBody}
	live := liveRequest(t, http.MethodPost, written.URL, fakeBody)

	require.True(t, matchRequest(live, written))

	raw, err := io.ReadAll(live.Body)
	require.NoError(t, err)
	require.Equal(t, fakeBody, string(raw), "the round trip must still find the body")
}

// The examples hand Transport() to the provider factory, so every call the provider makes must
// reach the request log and the cassette. Recorder() answers the recorder the fixture built.
func TestTransportFeedsTheRequestLogAndTheCassette(t *testing.T) {
	dir := t.TempDir()
	const name = "TestTransportLog"
	server := vendorFake(t)

	fixture, err := newFixture(Record, dir, name)
	require.NoError(t, err)
	require.NotNil(t, fixture.Recorder(), "the fixture must answer the recorder it built")
	require.Equal(t, recorder.ModeRecordOnly, fixture.Recorder().Mode())

	address := server.URL + "/inboxes/" + fakeInboxID
	_, err = sendCall(t, &http.Client{Transport: fixture.Transport()}, http.MethodGet, address, "")
	require.NoError(t, err)
	require.NoError(t, fixture.Stop())

	require.Equal(t, []string{"GET " + address}, fixture.Requests(),
		"a call sent through Transport must reach the request log")
	require.FileExists(t, filepath.Join(dir, name+".yaml"),
		"a call sent through Transport must reach the cassette")
}

// countingTransport counts the calls that reach it, and passes each one on.
type countingTransport struct {
	next  http.RoundTripper
	calls atomic.Int64
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.next.RoundTrip(r)
}

// A cassette that belongs to a suite rather than to one test takes its name from the caller, and
// the caller stops it. The integration suite records the inbox its tests share this way, and it
// names the transport of the process, so the call stays out of the cassette of whichever test
// asked for the inbox first.
func TestNewForNamesTheCassetteAndTheTransportOfARecordRun(t *testing.T) {
	dir := t.TempDir()
	const name = "TestMain_shared_inbox"
	server := vendorFake(t)
	address := server.URL + "/inboxes/" + fakeInboxID

	vendor := &countingTransport{next: http.DefaultTransport}
	recording, err := NewFor(Record, dir, name, vendor)
	require.NoError(t, err)
	recordedName := recording.Seed().Hex(4)
	_, err = sendCall(t, recording.Client(), http.MethodGet, address, "")
	require.NoError(t, err)
	require.NoError(t, recording.Stop(), "the caller stops the recorder of a suite")

	require.EqualValues(t, 1, vendor.calls.Load(),
		"a record run must send through the transport the caller named")
	require.FileExists(t, filepath.Join(dir, name+".yaml"), "the caller must name the cassette")
	require.FileExists(t, SeedPath(dir, name), "the seed belongs to the cassette the caller named")
	server.Close()

	unused := &countingTransport{next: http.DefaultTransport}
	replaying, err := NewFor(Replay, dir, name, unused)
	require.NoError(t, err)
	require.Equal(t, recordedName, replaying.Seed().Hex(4), "replay must draw the recorded name")
	_, err = sendCall(t, replaying.Client(), http.MethodGet, address, "")
	require.NoError(t, err, "the cassette the caller named must answer with the vendor stopped")
	require.NoError(t, replaying.Stop())
	require.Zero(t, unused.calls.Load(), "a replay must reach no transport")
}

// A suite that names a cassette nobody recorded gets the same refusal a test gets.
func TestNewForRefusesAReplayWithNoCassette(t *testing.T) {
	dir := t.TempDir()

	_, err := NewFor(Replay, dir, "TestMain_absent", http.DefaultTransport)
	require.Error(t, err, "replay with no cassette must fail")
	require.ErrorContains(t, err, filepath.Join(dir, "TestMain_absent.yaml"))
	require.ErrorContains(t, err, "make record_integration")
}

// secretBody is the write a caller sends when the account issues a credential. It names one
// credential at the top, one inside a nested object, and two inside an array.
const secretBody = `{"name":"pulumi-test-inbox","apiKey":"fake-secret-one",` +
	`"basicAuth":{"password":"fake-secret-two"},` +
	`"webhooks":[{"token":"fake-secret-three"},{"awsSecretKey":"fake-secret-four"}]}`

// theSecretsOfTheBody are the values the scrub must take out of secretBody.
var theSecretsOfTheBody = []string{
	"fake-secret-one", "fake-secret-two", "fake-secret-three", "fake-secret-four",
}

// The scrub reads the body of the request and the body of the response, on every path, so a
// credential a body carries never reaches the disk. A member that names no credential keeps its
// value, because the matcher compares the body of a write.
func TestTheScrubRedactsEverySecretMemberOfARequestAndAResponse(t *testing.T) {
	interaction := &cassette.Interaction{}
	interaction.Request.Method = http.MethodPost
	interaction.Request.URL = vendorBase + "/inboxes"
	interaction.Request.Headers = http.Header{"X-Api-Key": []string{fakeKey}}
	interaction.Request.Body = secretBody
	interaction.Response.Headers = http.Header{"Set-Cookie": []string{"session=opaque"}}
	interaction.Response.Body = secretBody

	require.NoError(t, scrub(interaction))

	for what, body := range map[string]string{
		"the request":  interaction.Request.Body,
		"the response": interaction.Response.Body,
	} {
		for _, secret := range theSecretsOfTheBody {
			require.NotContainsf(t, body, secret, "%s must carry no credential", what)
		}
		require.Containsf(t, body, `"name":"pulumi-test-inbox"`,
			"%s must keep the member that names no credential", what)
		require.Equalf(t, 4, strings.Count(body, RedactedValue),
			"%s must redact the member at the top, the nested one and both inside the array", what)
	}
}

// secretArrayBody names one credential member whose value is a list of strings, which is what an
// endpoint that answers more than one key writes.
//
//nolint:gosec // G101: every value here is a fake, and no account issued one of them.
const secretArrayBody = `{"name":"pulumi-test-inbox","token":["fake-secret-one","fake-secret-two"]}`

// A member that holds a list of credentials loses every one of them, so a body that carries two
// keys in one array leaves neither of them on the disk.
func TestTheScrubRedactsEveryStringOfASecretArray(t *testing.T) {
	redacted := RedactBody(secretArrayBody)

	for _, secret := range []string{"fake-secret-one", "fake-secret-two"} {
		require.NotContains(t, redacted, secret, "the array must carry no credential")
	}
	require.Equal(t, 2, strings.Count(redacted, RedactedValue),
		"every string of the array must be redacted")
	require.Contains(t, redacted, `"name":"pulumi-test-inbox"`,
		"a member that names no credential keeps its value")
}

// requireTheLengthOfTheBody reads the length one part of an interaction reports and the length it
// holds.
func requireTheLengthOfTheBody(t *testing.T, what, body string, length int64, headers http.Header) {
	t.Helper()
	require.EqualValuesf(t, len(body), length,
		"%s must report the length of the body it holds", what)
	require.Equalf(t, strconv.Itoa(len(body)), headers.Get(contentLengthHeader),
		"the header of %s must name the length of the body it holds", what)
}

// The scrub shortens a body that names a credential. A cassette that kept the recorded length
// would send a replayed reader after bytes the file does not hold.
func TestTheScrubKeepsTheLengthOfTheBodyItRedacted(t *testing.T) {
	recordedLength := strconv.Itoa(len(secretBody))
	interaction := &cassette.Interaction{}
	interaction.Request.Method = http.MethodPost
	interaction.Request.URL = vendorBase + "/inboxes"
	interaction.Request.Headers = http.Header{contentLengthHeader: []string{recordedLength}}
	interaction.Request.Body = secretBody
	interaction.Request.ContentLength = int64(len(secretBody))
	interaction.Response.Headers = http.Header{contentLengthHeader: []string{recordedLength}}
	interaction.Response.Body = secretBody
	interaction.Response.ContentLength = int64(len(secretBody))

	require.NoError(t, scrub(interaction))

	requireTheLengthOfTheBody(t, "the request", interaction.Request.Body,
		interaction.Request.ContentLength, interaction.Request.Headers)
	requireTheLengthOfTheBody(t, "the response", interaction.Response.Body,
		interaction.Response.ContentLength, interaction.Response.Headers)
}

// The cassette holds the redacted write, and the live request still carries the credential. The
// matcher redacts the live body the same way, so a recorded write matches the request that made it.
func TestTheMatcherRedactsTheLiveBodyBeforeItCompares(t *testing.T) {
	recorded := cassette.Request{
		Method: http.MethodPost,
		URL:    vendorBase + "/inboxes",
		Body:   RedactBody(secretBody),
	}
	for _, secret := range theSecretsOfTheBody {
		require.NotContains(t, recorded.Body, secret, "the cassette holds the redacted write")
	}

	require.True(t, matchRequest(liveRequest(t, http.MethodPost, recorded.URL, secretBody), recorded),
		"a live write that carries the credential must match the redacted recording")

	other := strings.Replace(secretBody, "pulumi-test-inbox", "another-inbox", 1)
	require.False(t, matchRequest(liveRequest(t, http.MethodPost, recorded.URL, other), recorded),
		"the redaction reads the credential alone, so another write must not match")
}
