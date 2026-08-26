//go:build yaml || nodejs || python || dotnet || go || all

package examples

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi/pkg/v3/testing/integration"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
)

// The program every language leg deploys. It attaches to the shared inbox, so a leg of any
// language creates no inbox of its own.

// webhookURLFor belongs to the reserved documentation domain, so no real server is ever called.
// The name rides the path, because the API refuses a second webhook of one inbox on the same URL.
func webhookURLFor(name string) string { return "https://example.com/mailslurp/" + name }

func cwd(t *testing.T) string {
	t.Helper()
	here, err := os.Getwd()
	require.NoError(t, err)
	return here
}

// requireObjectExists reads one object back from the account. A stack output alone would pass
// while the provider answered an identifier the account never gave it.
func requireObjectExists(t *testing.T, client *http.Client, key, path, what string) []byte {
	t.Helper()
	status, body, err := apiCall(context.Background(), client, key, http.MethodGet, path, nil, nil)
	require.NoErrorf(t, err, "the account should answer for the %s", what)
	require.Equalf(t, http.StatusOK, status, "the account should hold the %s", what)
	return body
}

func requireStringOutput(t *testing.T, stack integration.RuntimeValidationStackInfo, name string) string {
	t.Helper()
	value, ok := stack.Outputs[name].(string)
	require.Truef(t, ok, "the %s output should be a string, and it is a %T", name, stack.Outputs[name])
	require.NotEmptyf(t, value, "the %s output should carry a value", name)
	return value
}

// sweepLeg removes what one leg left behind and reports it. The stack destroy of the program test
// runs first, so a removal here is a leak rather than ordinary cleanup. No cassette holds this
// sweep, so it runs outside the replay mode alone; a recorded leg takes sweepLegThrough.
func sweepLeg(t *testing.T, key string, markers map[string]string) {
	t.Helper()
	ctx := context.Background()
	for _, class := range accountClasses() {
		marker, wanted := markers[class.name]
		if !wanted {
			continue
		}
		if removed := class.sweepMarked(ctx, key, marker); removed > 0 {
			t.Errorf("the program left %d %s behind, which the sweep has now removed", removed, class.name)
		}
	}
}

// sweepLegThrough removes what one recorded leg left behind, through the client of that leg. The
// cleanup runs before the recorder of the leg stops, so a record run writes these calls into the
// cassette and a replay sends the same ones again.
func sweepLegThrough(t *testing.T, client *http.Client, key string, markers map[string]string) {
	t.Helper()
	ctx := context.Background()
	for _, class := range accountClasses() {
		marker, wanted := markers[class.name]
		if !wanted {
			continue
		}
		if removed := class.sweepMarkedThrough(ctx, client, key, marker); removed > 0 {
			t.Errorf("the program left %d %s behind, which the sweep has now removed", removed, class.name)
		}
	}
}

// baseProgramOptions gives one base program the shared inbox, a name per object that the sweeps
// match, and the cleanup that runs when the program test fails before its own destroy.
func baseProgramOptions(t *testing.T, fixture *replaytest.Fixture, program string,
) integration.ProgramTestOptions {
	t.Helper()
	key := requireAPIKey(t)
	// The source declares no inbox, so no leg of any language pays for one.
	requireInboxBudget(t, baseSource, 0)

	inboxID := theSharedInbox(t)
	webhookName := newTestName(t, fixture.Seed(), webhookKind)
	rulesetTarget := rulesetTargetFor(newTestName(t, fixture.Seed(), rulesetKind))
	templateName := newTestName(t, fixture.Seed(), templateKind)

	t.Cleanup(func() {
		sweepLegThrough(t, fixture.Client(), key, map[string]string{
			webhookClass:  webhookName,
			rulesetClass:  rulesetTarget,
			templateClass: templateName,
		})
	})

	return integration.ProgramTestOptions{
		Dir: filepath.Join(cwd(t), program),
		Config: map[string]string{
			"inboxId":       inboxID,
			"webhookName":   webhookName,
			"webhookUrl":    webhookURLFor(webhookName),
			"rulesetTarget": rulesetTarget,
			"templateName":  templateName,
		},
		ExtraRuntimeValidation: func(t *testing.T, stack integration.RuntimeValidationStackInfo) {
			requireTheBaseProgramOnTheAccount(t, fixture.Client(), stack, program, key, inboxID)
		},
	}
}

// toTheFake sends every call to a local server. It routes a copy, so the request the recorder
// writes still names the vendor host and a replay of it matches.
type toTheFake struct{ address *url.URL }

func (f toTheFake) RoundTrip(r *http.Request) (*http.Response, error) {
	routed := r.Clone(r.Context())
	routed.URL.Scheme = f.address.Scheme
	routed.URL.Host = f.address.Host
	routed.Host = f.address.Host
	return http.DefaultTransport.RoundTrip(routed)
}

// refusingTransport answers every request with a refusal. A replay reads a cassette and sends
// nothing, so a proof below hands this one to every replay: a build that reached a transport at all
// would be reaching the account, and this reaches nothing.
type refusingTransport struct{}

func (refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("this transport sends no request")
}

// theCreatedInboxID is the identifier the fake answers a create with.
const theCreatedInboxID = "0690943a-5b02-41b5-b80a-d486a256215a"

// inboxCreateFake answers one inbox create, the way the API does.
func inboxCreateFake(t *testing.T) http.RoundTripper {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"`+theCreatedInboxID+`"}`)
	}))
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	return toTheFake{address}
}

// recordTheSharedInbox writes the cassette of the shared inbox into one directory, against a local
// fake, and answers the name that recording drew.
func recordTheSharedInbox(t *testing.T, dir string) string {
	t.Helper()
	fixture, name, id, err := buildTheSharedInbox(replaytest.Record, noAccountKey, dir,
		inboxCreateFake(t))
	require.NoError(t, err)
	require.Equal(t, theCreatedInboxID, id)
	require.NoError(t, fixture.Stop())
	require.FileExists(t, filepath.Join(dir, sharedInboxCassette+".yaml"),
		"the record run must write the cassette the suite names")
	return name
}

// The shared inbox belongs to the suite, so it reads a cassette that carries its own name. A run
// filtered down to one leg then still finds the inbox, whichever leg the recording built it under.
func TestTheSharedInboxReadsTheCassetteThatCarriesItsName(t *testing.T) {
	dir := t.TempDir()
	recorded := recordTheSharedInbox(t, dir)

	// The replay sends through a transport that refuses every call, so it answers from the
	// cassette or it answers nothing at all.
	fixture, name, id, err := buildTheSharedInbox(replaytest.Replay, noAccountKey, dir,
		refusingTransport{})
	require.NoError(t, err, "the replay must read the cassette of the shared inbox")
	assert.Equal(t, recorded, name, "the name must come from the seed of that cassette")
	assert.Equal(t, theCreatedInboxID, id, "the cassette must answer the identifier it recorded")
	require.NoError(t, fixture.Stop(), "the replay must consume the recorded create")
}

// The name of the shared inbox rides the create body, so a replay matches its recording only when
// the seed of the cassette draws the same name every time.
func TestTheSharedInboxDrawsTheSameNameFromTheSameSeed(t *testing.T) {
	dir := t.TempDir()
	recorded := recordTheSharedInbox(t, dir)

	for _, build := range []string{"the first build", "the second build"} {
		fixture, name, _, err := buildTheSharedInbox(replaytest.Replay, noAccountKey, dir,
			refusingTransport{})
		require.NoErrorf(t, err, "%s must read the cassette", build)
		assert.Equalf(t, recorded, name, "%s must draw the recorded name", build)
		require.NoError(t, fixture.Stop())
	}
}

// A replay that finds no cassette for the shared inbox must name the file and the target that
// writes it, the same way a leg does.
func TestTheSharedInboxRefusesAReplayWithNoCassette(t *testing.T) {
	fixture, name, id, err := buildTheSharedInbox(replaytest.Replay, noAccountKey, t.TempDir(),
		refusingTransport{})
	require.Error(t, err, "a replay must never create the shared inbox")
	assert.ErrorContains(t, err, sharedInboxCassette+".yaml",
		"the refusal must name the cassette the suite reads")
	assert.ErrorContains(t, err, "make record_examples",
		"the refusal must name the target that writes it")
	assert.Nil(t, fixture)
	assert.Empty(t, name)
	assert.Empty(t, id)
}

// theClassNamed answers one class of account object by name.
func theClassNamed(t *testing.T, name string) objectClass {
	t.Helper()
	for _, class := range accountClasses() {
		if class.name == name {
			return class
		}
	}
	require.FailNowf(t, "unknown class", "this package holds no %s class", name)
	return objectClass{}
}

// sweepFake answers the list of one marked webhook and the delete of it, and counts the deletes.
// The server answers on its own goroutine, so the count is atomic.
func sweepFake(t *testing.T, marker string, deletes *atomic.Int64) http.RoundTripper {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"id":"webhook-1","name":"`+marker+`"}]}`)
	}))
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	return toTheFake{address}
}

// A cleanup a leg registers runs before that leg's recorder stops, so a record run writes the
// calls of the sweep into the cassette of the leg. The replay must send the same calls again: a
// sweep that read the mode and sent none would leave the recorder holding a call nobody made.
func TestASweepALegRegistersRunsInTheReplayMode(t *testing.T) {
	dir := t.TempDir()
	const cassetteName = "TestALegSweep"
	marker := testNamePrefix + webhookKind + "-abcd1234"
	class := theClassNamed(t, webhookClass)
	ctx := context.Background()

	var deletes atomic.Int64
	recording, err := replaytest.NewFor(replaytest.Record, dir, cassetteName,
		sweepFake(t, marker, &deletes))
	require.NoError(t, err)
	require.Equal(t, 1, class.sweepMarkedThrough(ctx, recording.Client(), noAccountKey, marker),
		"the recorded sweep must remove the marked webhook")
	require.NoError(t, recording.Stop())
	require.EqualValues(t, 1, deletes.Load())

	t.Setenv(replaytest.ModeVariable, string(replaytest.Replay))
	replaying, err := replaytest.NewFor(replaytest.Replay, dir, cassetteName, refusingTransport{})
	require.NoError(t, err)
	assert.Equal(t, 1, class.sweepMarkedThrough(ctx, replaying.Client(), noAccountKey, marker),
		"the replayed sweep must remove the webhook the recording removed")
	require.NoError(t, replaying.Stop(),
		"the replay must send every call the recorded sweep made")
	assert.EqualValues(t, 1, deletes.Load(), "a replayed sweep reaches the cassette and no account")
}

// theSuiteKeepsItsSharedInbox saves what the suite holds about the shared inbox, and puts all of
// it back when the calling test ends. A proof that drives the entry point of the suite then leaves
// the legs the inbox, the budget and the builder they would have found.
func theSuiteKeepsItsSharedInbox(t *testing.T) {
	t.Helper()
	inbox, fixture, builder := sharedInbox, theSharedInboxFixture, theSharedInboxBuilder
	spent := inboxCreations.Load()
	t.Cleanup(func() {
		sharedInbox, theSharedInboxFixture, theSharedInboxBuilder = inbox, fixture, builder
		inboxCreations.Store(spent)
	})
	sharedInbox.once = new(sync.Once)
}

// The suite hands the builder the cassette directory of this package and the transport of the
// process. A leg that handed its own recorder instead would write the create into the cassette of
// whichever leg asked first, and a run filtered down to another leg would then find no inbox.
func TestTheSuiteHandsTheBuilderItsCassetteDirectoryAndItsTransport(t *testing.T) {
	t.Setenv(replaytest.ModeVariable, string(replaytest.Replay))
	theSuiteKeepsItsSharedInbox(t)

	const answered = "the identifier this proof answers"
	var seenDir string
	var seenVendor http.RoundTripper
	theSharedInboxBuilder = func(_ replaytest.Mode, _, dir string, vendor http.RoundTripper,
	) (*replaytest.Fixture, string, string, error) {
		seenDir, seenVendor = dir, vendor
		return nil, "the name this proof answers", answered, nil
	}

	require.Equal(t, answered, theSharedInbox(t), "the suite answers what the builder built")
	assert.Equal(t, cassetteDir, seenDir,
		"the suite must name the cassette directory of this package")
	assert.Same(t, theVendorTransport, seenVendor,
		"the suite must name the transport of the process")
}

// forwarderProbeFake answers the entitlement probe the way an account whose plan carries no
// forwarder does, lists the forwarder the probe named, and counts the deletes the sweep sends. The
// server answers on its own goroutine, so both values are atomic.
func forwarderProbeFake(t *testing.T, named *atomic.Pointer[string], deletes *atomic.Int64,
) http.RoundTripper {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodDelete:
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			var sent struct {
				ForwardToRecipients []string `json:"forwardToRecipients"`
			}
			if err := json.NewDecoder(r.Body).Decode(&sent); err == nil &&
				len(sent.ForwardToRecipients) == 1 {
				named.Store(&sent.ForwardToRecipients[0])
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"errorCode":"`+errorCodeSubscriptionLimit+`"}`)
		default:
			recipient := ""
			if stored := named.Load(); stored != nil {
				recipient = *stored
			}
			_, _ = io.WriteString(w,
				`{"content":[{"id":"forwarder-1","forwardToRecipients":["`+recipient+`"]}]}`)
		}
	}))
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	return toTheFake{address}
}

// The probe writes a forwarder, so it registers a sweep. That cleanup runs before the recorder of
// its test stops, so a record run writes the sweep into the cassette and a replay must send the
// same calls again. This proof runs in the replay mode: a sweep that read the mode and sent
// nothing would leave the recorder holding a call nobody made.
func TestTheEntitlementProbeSweepsThroughTheClientOfItsTest(t *testing.T) {
	t.Setenv(replaytest.ModeVariable, string(replaytest.Replay))

	var named atomic.Pointer[string]
	var deletes atomic.Int64
	fixture, err := replaytest.NewFor(replaytest.Record, t.TempDir(), "TestTheProbe",
		forwarderProbeFake(t, &named, &deletes))
	require.NoError(t, err)

	// The sweep the probe registers runs when the subtest ends, which is before the reads below.
	var reason string
	t.Run("the probe", func(t *testing.T) {
		reason = probeTheForwarderEntitlement(t, fixture, noAccountKey, theCreatedInboxID)
	})
	require.NoError(t, fixture.Stop())

	assert.Equal(t, noForwarderEntitlementReason, reason, "the plan refusal skips the program")
	assert.EqualValues(t, 1, deletes.Load(), "the sweep must remove the forwarder the probe named")
	assert.Contains(t, fixture.Requests(), "DELETE "+baseURL+"/forwarders/forwarder-1",
		"the sweep must send its calls through the client of the test")
}

// requireTheBaseProgramOnTheAccount reads every object of the deployed program back from MailSlurp.
func requireTheBaseProgramOnTheAccount(t *testing.T, client *http.Client,
	stack integration.RuntimeValidationStackInfo, program, key, inboxID string,
) {
	t.Helper()
	surveyTheAccount(t, key, "while the "+program+" program is deployed")
	chargeInboxes(t, countInboxResources(stack), program)
	assert.Zerof(t, countInboxResources(stack), "the %s program should hold no inbox", program)

	inbox := requireObjectExists(t, client, key, "/inboxes/"+inboxID, "shared inbox")
	var read struct {
		EmailAddress string `json:"emailAddress"`
	}
	require.NoError(t, json.Unmarshal(inbox, &read))
	assert.Equal(t, read.EmailAddress, requireStringOutput(t, stack, "emailAddress"),
		"the inbox function should answer the address the account gives the inbox")

	requireObjectExists(t, client, key,
		"/webhooks/"+requireStringOutput(t, stack, "webhookId"), "webhook")
	requireObjectExists(t, client, key,
		"/rulesets/"+requireStringOutput(t, stack, "rulesetId"), "ruleset")

	variables, ok := stack.Outputs["templateVariables"].([]any)
	require.Truef(t, ok, "the template variables should be a list, and they are a %T",
		stack.Outputs["templateVariables"])
	assert.Len(t, variables, 2, "the template content declares two variables")
}
