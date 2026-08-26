package examples

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/pulumi/providertest/providers"
	"github.com/pulumi/pulumi/pkg/v3/testing/integration"
	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	"github.com/jschady/pulumi-mailslurp/internal/replaytest"
	"github.com/jschady/pulumi-mailslurp/provider"
)

// The suite entry point, the one shared inbox and the budget that counts every inbox a run creates.
// This file carries no build tag, so every language leg reads it.

// baseSource is the hand-written program the four language directories are converted from, and
// completeSource is the one that covers every resource and both functions.
const (
	baseSource     = "base"
	completeSource = "yaml"
)

// inboxResourceType is the resource whose creations are billed.
const inboxResourceType = "mailslurp:index:Inbox"

// maxInboxCreations is the budget of a full run. MailSlurp bills every inbox and burns a rolling
// quota slot that a delete does not refund.
const maxInboxCreations = 3

var inboxCreations atomic.Int64

// chargeInboxes records what one program created and fails the run past the budget.
func chargeInboxes(t *testing.T, count int64, program string) {
	t.Helper()
	if count == 0 {
		return
	}
	spent := inboxCreations.Add(count)
	t.Logf("the %s program created %d of the %d inboxes this run may create", program, count, maxInboxCreations)
	require.LessOrEqualf(t, spent, int64(maxInboxCreations),
		"the run created %d inboxes, and the budget is %d", spent, maxInboxCreations)
}

// countInboxResources answers how many inboxes one deployed stack holds. Every leg reads it, so an
// inbox that reaches a program which should hold none is a failed run rather than a silent bill.
func countInboxResources(stack integration.RuntimeValidationStackInfo) int64 {
	if stack.Deployment == nil {
		return 0
	}
	var counted int64
	for _, resource := range stack.Deployment.Resources {
		if string(resource.Type) == inboxResourceType {
			counted++
		}
	}
	return counted
}

// countInboxDeclarations answers how many inboxes one program declares. The count is read before
// the program runs, so a program that would overspend is refused rather than billed.
func countInboxDeclarations(t *testing.T, program string) int64 {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(program, "Pulumi.yaml"))
	require.NoErrorf(t, err, "the %s program should hold a project file", program)

	// The whole line is compared, because the ruleset and the forwarder both start with the token
	// of the inbox and a prefix match would count them too.
	var counted int64
	for _, line := range strings.Split(string(source), "\n") {
		if strings.TrimSpace(line) == "type: "+inboxResourceType {
			counted++
		}
	}
	return counted
}

// requireInboxBudget refuses a program that declares more inboxes than the caller expects.
func requireInboxBudget(t *testing.T, program string, want int64) {
	t.Helper()
	require.Equalf(t, want, countInboxDeclarations(t, program),
		"the %s program should declare %d inboxes", program, want)
}

// sharedInboxCassette names the cassette the shared inbox belongs to. Whichever leg asks for the
// inbox first causes the create, so those calls belong to the suite and not to that leg. A leg the
// run filters out then still finds the inbox, because it reads a cassette of its own name.
const sharedInboxCassette = "SharedInbox"

// theVendorTransport is the transport of the process. A record run of the shared inbox sends
// through it, and the teardown that removes the inbox names it too.
var theVendorTransport = http.DefaultTransport

// sharedInbox is the one inbox the programs that attach to an inbox all reuse. It is built the
// first time a leg asks, so a run that asks for none pays for none.
var sharedInbox = struct {
	// once builds the inbox one time in the process. It is a pointer, so the proof that reads
	// what the suite hands the builder can give the suite a fresh one and put this one back.
	once *sync.Once
	id   string
	// name is set before the create, so an inbox the API made and then reported as a failure is
	// still swept in teardown.
	name string
	err  error
}{once: new(sync.Once)}

// theSharedInboxBuilder is the builder the suite calls. A proof replaces it to read the cassette
// directory and the transport the suite hands the builder, and puts it back when it ends.
var theSharedInboxBuilder = buildTheSharedInbox

// theSharedInboxFixture holds the recorder of the cassette the shared inbox belongs to. A run that
// asked for no inbox builds none, and the suite then closes nothing.
var theSharedInboxFixture *replaytest.Fixture

// theSharedInbox answers the shared inbox, building it on first use.
func theSharedInbox(t *testing.T) string {
	t.Helper()
	key := requireAPIKey(t)
	mode := replaytest.ModeOf(t)
	// The suite hands the builder the cassette directory of this package and the transport of the
	// process, so the create belongs to the suite and not to the leg that asked for it first.
	sharedInbox.once.Do(func() {
		chargeInboxes(t, 1, "shared inbox")
		theSharedInboxFixture, sharedInbox.name, sharedInbox.id, sharedInbox.err =
			theSharedInboxBuilder(mode, key, cassetteDir, theVendorTransport)
	})
	require.NoError(t, sharedInbox.err, "the suite could not create the shared inbox")
	require.NotEmpty(t, sharedInbox.id, "the create of the shared inbox answered no identifier")
	return sharedInbox.id
}

// sharedInboxBody is the create the suite sends. The name rides it, so a replay only matches the
// recording when it draws the name the recording drew.
func sharedInboxBody(name string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"name":        name,
		"description": "The shared inbox of the example programs.",
	})
}

// buildTheSharedInbox creates the one inbox the example programs share. The create belongs to the
// suite and not to whichever leg asked for the inbox first, so it goes through a cassette of its
// own. A record run sends it through the transport of the process, so it stays out of the cassette
// of that leg, and a replay reads the cassette of the suite and reaches no account.
func buildTheSharedInbox(mode replaytest.Mode, key, dir string, vendor http.RoundTripper,
) (fixture *replaytest.Fixture, name, id string, err error) {
	fixture, err = replaytest.NewFor(mode, dir, sharedInboxCassette, vendor)
	if err != nil {
		return nil, "", "", err
	}

	// The name comes from the seed of that cassette, so a replay sends the body the recording
	// holds. The suite draws the name before the create, so an inbox the API made and then
	// reported as a failure still leaves a name the teardown sweeps.
	name = nameFromSeed(fixture.Seed(), inboxKind)
	body, err := sharedInboxBody(name)
	if err != nil {
		return fixture, name, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	status, answered, err := apiCall(ctx, fixture.Client(), key, http.MethodPost, "/inboxes", nil, body)
	if err != nil {
		return fixture, name, "", err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fixture, name, "", fmt.Errorf("the inbox create answered the status %d", status)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(answered, &created); err != nil {
		return fixture, name, "", err
	}
	return fixture, name, created.ID, nil
}

// stopTheSharedInboxRecorder closes the cassette of the shared inbox after the last leg. It reads
// the recorder after the run, because a leg builds it while the run is going on. A record run
// writes the file here, and a replay reports a recorded call the suite never sent.
func stopTheSharedInboxRecorder(code int) int {
	if theSharedInboxFixture == nil {
		return code
	}
	if err := theSharedInboxFixture.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "the cassette of the shared inbox failed: %v\n", err)
		return 1
	}
	return code
}

// removeTheSharedInbox clears the shared inbox after the run. It deletes the identifier the create
// answered, then sweeps the name: a retried create can leave a second inbox this process never saw.
// A replay created no inbox, so it removes none.
func removeTheSharedInbox(ctx context.Context, key string) {
	if !callsTheAccount() {
		fmt.Fprintln(os.Stderr, "the replay mode reads a cassette, so no shared inbox is removed")
		return
	}
	if sharedInbox.id != "" {
		status, _, err := apiCall(ctx, vendorClient(), key, http.MethodDelete,
			"/inboxes/"+sharedInbox.id, nil, nil)
		if err != nil || (status != http.StatusOK && status != http.StatusNoContent &&
			status != http.StatusNotFound) {
			fmt.Fprintf(os.Stderr, "the teardown could not delete the shared inbox: %v %d\n", err, status)
		}
	}
	if sharedInbox.name == "" {
		return
	}
	for _, class := range accountClasses() {
		if class.name == inboxClass {
			class.sweepMarked(ctx, key, sharedInbox.name)
		}
	}
}

// passphraseVariable protects every secret the state holds, and the provider configuration is one.
//
//nolint:gosec // G101: this names the environment variable, and holds no passphrase.
const passphraseVariable = "PULUMI_CONFIG_PASSPHRASE"

// newRunPassphrase answers a passphrase for this run alone. crypto/rand.Read fills the buffer or
// panics inside the runtime, so it reports no error worth handling.
func newRunPassphrase() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// localBackend points the Pulumi CLI at a state directory inside this repository. A run that
// reaches a cloud backend would ask for a login the suite has no business holding.
func localBackend() error {
	const backendVariable = "PULUMI_BACKEND_URL"
	address := os.Getenv(backendVariable)
	if address == "" {
		root, err := filepath.Abs("..")
		if err != nil {
			return err
		}
		address = "file://" + filepath.Join(root, ".pulumi-state")
		if err := os.Setenv(backendVariable, address); err != nil {
			return err
		}
	}
	// The state of a stack carries the provider configuration, and the MailSlurp API key is part
	// of it. A passphrase that lives only in this process leaves nothing on disk anyone can read.
	if err := os.Setenv(passphraseVariable, newRunPassphrase()); err != nil {
		return err
	}
	if !strings.HasPrefix(address, "file://") {
		return nil
	}
	//nolint:gosec // G703: the directory is the state backend this repository names, not user input.
	return os.MkdirAll(strings.TrimPrefix(address, "file://"), 0o750)
}

func TestMain(m *testing.M) { os.Exit(runExampleSuite(m)) }

func runExampleSuite(m *testing.M) int {
	if err := localBackend(); err != nil {
		fmt.Fprintf(os.Stderr, "the suite could not prepare the state directory: %v\n", err)
		return 1
	}

	if key := apiKey(); key != "" {
		if counted, err := readAccount(context.Background(), key); err == nil && counted != nil {
			fmt.Fprintln(os.Stderr, surveyLine("before the run", counted))
		}
	}

	code := stopTheSharedInboxRecorder(m.Run())

	key := apiKey()
	if key == "" {
		return code
	}
	ctx := context.Background()
	removeTheSharedInbox(ctx, key)

	counted, err := readAccount(ctx, key)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "the suite could not read the account after the run: %v\n", err)
		code = 1
	case counted != nil:
		fmt.Fprintln(os.Stderr, surveyLine("after the run", counted))
	}

	spent := inboxCreations.Load()
	fmt.Fprintf(os.Stderr, "the example programs created %d inboxes, and the budget is %d\n",
		spent, maxInboxCreations)
	if spent > maxInboxCreations {
		return 1
	}
	return code
}

// baseOptions is what every language leg starts from. A live run reads the provider from the
// binary this repository built, so no plugin is downloaded and no published version is reached. A
// replay and a record attach the provider this test process serves, which the recorder reaches.
func baseOptions(t *testing.T) integration.ProgramTestOptions {
	t.Helper()
	home, err := filepath.Abs("../.pulumi")
	require.NoError(t, err)

	options := integration.ProgramTestOptions{
		PulumiHomeDir: home,
		// The roundtrip writes a second copy of the checkpoint, and the state of a credentialed
		// run holds the configuration of the provider, so this suite leaves the roundtrip out.
		SkipExportImport: true,
		// A leg that attaches the provider in process also writes the placeholder credential into
		// the environment, and Go refuses an environment write in a parallel test. A live run keeps
		// the parallel legs.
		NoParallel: attachesInProcess(),
	}
	if attachesInProcess() {
		return options
	}
	binPath, err := filepath.Abs("../bin")
	require.NoError(t, err)
	options.LocalProviders = []integration.LocalDependency{{Package: "mailslurp", Path: binPath}}
	return options
}

// requireTheFullLifecycle refuses options that dropped a step. The empty preview and update after
// the first one is the step that sends the provider the inputs the engine holds.
func requireTheFullLifecycle(t *testing.T, options integration.ProgramTestOptions) {
	t.Helper()
	require.False(t, options.SkipEmptyPreviewUpdate,
		"the second preview and update is the only step that sends the engine's own inputs")
	require.False(t, options.SkipRefresh, "the refresh proves the account matches the state")
	require.False(t, options.ExpectRefreshChanges, "the refresh should find nothing to change")
	require.False(t, options.SkipPreview)
	require.False(t, options.SkipUpdate)
	require.False(t, options.Quick, "the quick mode turns off the preview and the second update")
	require.True(t, options.SkipExportImport, "an exported checkpoint would reach the disk")
}

// runProgram drives one example program. Every leg goes through here, so a step turned off
// anywhere between the shared options and the leg itself fails that leg before it reaches the API.
func runProgram(t *testing.T, fixture *replaytest.Fixture, options integration.ProgramTestOptions) {
	t.Helper()
	requireTheFullLifecycle(t, options)
	options.Env = append(options.Env, theProviderOfThisRun(t, fixture, options.Dir)...)
	integration.ProgramTest(t, &options)
}

// programSource tells the provider factory which program the leg deploys.
type programSource string

func (s programSource) Source() string { return string(s) }

// debugProvidersVariable points the CLI at a provider that already runs, so the engine starts no
// plugin for that package.
const debugProvidersVariable = "PULUMI_DEBUG_PROVIDERS"

// outlivesTheEngine serves one provider across every pulumi process of a leg. The engine sends
// Cancel when each process ends, and the cancel middleware of the provider then refuses every
// later call. A plugin binary dies with its process, so no call ever follows a Cancel there; the
// attached server ignores the call to behave the same way.
type outlivesTheEngine struct{ rpc.ResourceProviderServer }

func (outlivesTheEngine) Cancel(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// theProviderOfThisRun answers what one leg adds to its environment to reach the provider. A
// replay and a record serve the provider inside this test process and hand it the recorder of the
// leg, so every call it makes lands in the cassette. A live run reads the installed plugin, and
// adds nothing. No untagged test deploys a program, so this starts nothing without a build tag.
func theProviderOfThisRun(t *testing.T, fixture *replaytest.Fixture, source string) []string {
	t.Helper()
	if !attachesInProcess() {
		return nil
	}
	require.NotNil(t, fixture, "a leg that attaches the provider carries a recorder")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serve := providers.ResourceProviderFactory(
		func(providers.PulumiTest) (rpc.ResourceProviderServer, error) {
			server, err := provider.NewWith(fixture.Transport())(nil)
			if err != nil {
				return nil, err
			}
			return outlivesTheEngine{server}, nil
		})
	port, err := serve(ctx, programSource(source))
	require.NoError(t, err, "the test process should serve the provider")

	environment := []string{debugProvidersVariable + "=" + providers.GetDebugProvidersEnv(
		map[providers.ProviderName]providers.Port{providers.ProviderName(provider.Name): port})}
	if !callsTheAccount() {
		// The engine reads the key of the provider from the environment. A replayed call carries
		// no credential, so the leg runs on a value the account never issued.
		environment = append(environment, apiKeyVariable+"="+noAccountKey)
	}
	return environment
}

func nodejsOptions(t *testing.T) integration.ProgramTestOptions {
	t.Helper()
	return baseOptions(t).With(integration.ProgramTestOptions{
		Dependencies: []string{"pulumi-mailslurp"},
	})
}

func pythonOptions(t *testing.T) integration.ProgramTestOptions {
	t.Helper()
	return baseOptions(t).With(integration.ProgramTestOptions{
		Dependencies: []string{filepath.Join("..", "sdk", "python", "bin")},
	})
}

func dotnetOptions(t *testing.T) integration.ProgramTestOptions {
	t.Helper()
	return baseOptions(t).With(integration.ProgramTestOptions{
		Dependencies: []string{"Jschady.Mailslurp"},
	})
}

func goOptions(t *testing.T) integration.ProgramTestOptions {
	t.Helper()
	sdkPath, err := filepath.Abs("../sdk/go/mailslurp")
	require.NoError(t, err)
	depRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	return baseOptions(t).With(integration.ProgramTestOptions{
		Dependencies: []string{
			"github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp=" + sdkPath,
		},
		Env: []string{"PULUMI_GO_DEP_ROOT=" + depRoot},
	})
}

// TestTheLegsRunOneAtATimeWhenTheProviderAttachesInProcess pins the option that keeps the recorded
// legs serial. A parallel leg would panic on the environment write of the placeholder credential.
func TestTheLegsRunOneAtATimeWhenTheProviderAttachesInProcess(t *testing.T) {
	for mode, serial := range map[string]bool{"replay": true, "record": true, "live": false} {
		t.Setenv(replaytest.ModeVariable, mode)
		require.Equalf(t, serial, baseOptions(t).NoParallel, "the %s mode", mode)
	}
}
