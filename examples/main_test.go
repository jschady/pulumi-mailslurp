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

	"github.com/pulumi/pulumi/pkg/v3/testing/integration"
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

// sharedInbox is the one inbox the programs that attach to an inbox all reuse. It is built the
// first time a leg asks, so a run that asks for none pays for none.
var sharedInbox struct {
	once sync.Once
	id   string
	// name is set before the create, so an inbox the API made and then reported as a failure is
	// still swept in teardown.
	name string
	err  error
}

func theSharedInbox(t *testing.T) string {
	t.Helper()
	key := requireAPIKey(t)
	sharedInbox.once.Do(func() { buildTheSharedInbox(t, key) })
	require.NoError(t, sharedInbox.err, "the suite could not create the shared inbox")
	require.NotEmpty(t, sharedInbox.id, "the create of the shared inbox answered no identifier")
	return sharedInbox.id
}

func buildTheSharedInbox(t *testing.T, key string) {
	t.Helper()
	sharedInbox.name = newTestName(inboxKind)
	chargeInboxes(t, 1, "shared inbox")

	body, err := json.Marshal(map[string]any{
		"name":        sharedInbox.name,
		"description": "The shared inbox of the example programs.",
	})
	if err != nil {
		sharedInbox.err = err
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	status, answered, err := apiCall(ctx, key, http.MethodPost, "/inboxes", nil, body)
	if err != nil {
		sharedInbox.err = err
		return
	}
	if status != http.StatusOK && status != http.StatusCreated {
		sharedInbox.err = fmt.Errorf("the inbox create answered the status %d", status)
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(answered, &created); err != nil {
		sharedInbox.err = err
		return
	}
	sharedInbox.id = created.ID
}

// removeTheSharedInbox clears the shared inbox after the run. It deletes the identifier the create
// answered, then sweeps the name: a retried create can leave a second inbox this process never saw.
func removeTheSharedInbox(ctx context.Context, key string) {
	if sharedInbox.id != "" {
		status, _, err := apiCall(ctx, key, http.MethodDelete, "/inboxes/"+sharedInbox.id, nil, nil)
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
		if counted, err := readAccount(context.Background(), key); err == nil {
			fmt.Fprintln(os.Stderr, surveyLine("before the run", counted))
		}
	}

	code := m.Run()

	key := apiKey()
	if key == "" {
		return code
	}
	ctx := context.Background()
	removeTheSharedInbox(ctx, key)

	if counted, err := readAccount(ctx, key); err == nil {
		fmt.Fprintln(os.Stderr, surveyLine("after the run", counted))
	} else {
		fmt.Fprintf(os.Stderr, "the suite could not read the account after the run: %v\n", err)
		code = 1
	}

	spent := inboxCreations.Load()
	fmt.Fprintf(os.Stderr, "the example programs created %d inboxes, and the budget is %d\n",
		spent, maxInboxCreations)
	if spent > maxInboxCreations {
		return 1
	}
	return code
}

// baseOptions is what every language leg starts from. The provider comes from the binary this
// repository built, so no plugin is downloaded and no published version is reached.
func baseOptions(t *testing.T) integration.ProgramTestOptions {
	t.Helper()
	binPath, err := filepath.Abs("../bin")
	require.NoError(t, err)
	home, err := filepath.Abs("../.pulumi")
	require.NoError(t, err)

	return integration.ProgramTestOptions{
		LocalProviders: []integration.LocalDependency{{Package: "mailslurp", Path: binPath}},
		PulumiHomeDir:  home,
		// The roundtrip writes a second copy of the checkpoint, and the state of a credentialed
		// run holds the configuration of the provider, so this suite leaves the roundtrip out.
		SkipExportImport: true,
	}
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
func runProgram(t *testing.T, options integration.ProgramTestOptions) {
	t.Helper()
	requireTheFullLifecycle(t, options)
	integration.ProgramTest(t, &options)
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
