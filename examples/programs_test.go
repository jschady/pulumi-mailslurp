package examples

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi/pkg/v3/testing/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
)

// The pins over the committed programs. They read no credential, so they run in every build.

const schemaPath = "../provider/cmd/pulumi-resource-mailslurp/schema.json"

// languagePrograms names the file each conversion writes and the call it makes to the one
// function of the base program. Each language spells that call its own way.
func languagePrograms() map[string]struct{ file, inboxCall string } {
	return map[string]struct{ file, inboxCall string }{
		"nodejs": {"index.ts", "getInboxOutput("},
		"python": {"__main__.py", "get_inbox_output("},
		"dotnet": {"Program.cs", "GetInbox.Invoke("},
		// The Go SDK calls the function LookupInbox, because GetInbox already names the getter of
		// the Inbox resource. https://github.com/pulumi/pulumi/issues/22707
		"go": {"main.go", "LookupInboxOutput("},
	}
}

func readProgram(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	require.NoErrorf(t, err, "the repository should hold %s", path)
	return string(source)
}

// schemaItems answers every resource and function the committed schema declares.
func schemaItems(t *testing.T) []string {
	t.Helper()
	var schema struct {
		Resources map[string]json.RawMessage `json:"resources"`
		Functions map[string]json.RawMessage `json:"functions"`
	}
	require.NoError(t, json.Unmarshal([]byte(readProgram(t, schemaPath)), &schema))

	items := make([]string, 0, len(schema.Resources)+len(schema.Functions))
	for token := range schema.Resources {
		items = append(items, token)
	}
	for token := range schema.Functions {
		items = append(items, token)
	}
	slices.Sort(items)
	return items
}

// The complete program is the source of truth, so a resource or a function that the provider ships
// and it never names would go unexampled.
func TestTheCompleteProgramNamesEveryItemOfTheSchema(t *testing.T) {
	t.Parallel()
	program := readProgram(t, filepath.Join(completeSource, "Pulumi.yaml"))
	items := schemaItems(t)
	require.Len(t, items, 7, "the provider ships five resources and two functions")

	for _, token := range items {
		assert.Containsf(t, program, token, "the complete program should name %s", token)
	}
}

// The two deployable programs together carry everything the complete program carries, apart from
// the two items this account plan refuses. A drift between them would go unnoticed otherwise.
func TestTheDeployableProgramsPartitionTheCompleteProgram(t *testing.T) {
	t.Parallel()
	deployed := readProgram(t, filepath.Join(baseSource, "Pulumi.yaml")) +
		readProgram(t, filepath.Join("inbox", "Pulumi.yaml"))

	armed := []string{"mailslurp:index:InboxForwarder", "mailslurp:index:getDomain"}
	for _, token := range schemaItems(t) {
		if slices.Contains(armed, token) {
			assert.NotContainsf(t, deployed, token,
				"%s cannot be deployed here, so no deployable program names it", token)
			continue
		}
		assert.Containsf(t, deployed, token, "a deployable program should name %s", token)
	}
}

// The converter reports an unknown resource on the error stream and still exits with the code 0,
// leaving that resource out of the program it writes. So each conversion is read back here.
func TestEveryConversionCarriesEveryResourceOfTheBaseProgram(t *testing.T) {
	t.Parallel()
	source := readProgram(t, filepath.Join(baseSource, "Pulumi.yaml"))
	names := []string{"Webhook", "InboxRuleset", "EmailTemplate"}
	for _, name := range names {
		require.Containsf(t, source, "mailslurp:index:"+name, "the base program should name %s", name)
	}

	for language, program := range languagePrograms() {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			converted := readProgram(t, filepath.Join(language, program.file))
			for _, name := range names {
				assert.Containsf(t, converted, name, "the %s program should build a %s", language, name)
			}
			assert.Containsf(t, converted, program.inboxCall,
				"the %s program should call the inbox function", language)
			assert.Containsf(t, converted, provenanceMarker,
				"the %s program should say where it came from", language)
		})
	}
}

// provenanceMarker is what every converted program carries on its first line. A reader who opens
// one of them learns that the change belongs in the source program.
const provenanceMarker = "Converted from examples/base by make examples."

// The published package names of every manifest. A build version in any of them would resolve
// nowhere, because none of these packages is published yet.
func TestEveryManifestNamesThePublishedPackage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		manifest string
		names    string
		absent   []string
	}{
		{filepath.Join("nodejs", "package.json"), "@pulumi/pulumi", []string{"pulumi-mailslurp"}},
		{filepath.Join("python", "requirements.txt"), "pulumi>=", []string{"pulumi_mailslurp"}},
		{filepath.Join("dotnet", "mailslurp-base.csproj"), `"Jschady.Mailslurp" Version="*-*"`, nil},
		{filepath.Join("go", "go.mod"), "github.com/jschady/pulumi-mailslurp/sdk/go/mailslurp", nil},
	} {
		t.Run(tc.manifest, func(t *testing.T) {
			t.Parallel()
			manifest := readProgram(t, tc.manifest)
			assert.Contains(t, manifest, tc.names)
			for _, absent := range tc.absent {
				assert.NotContainsf(t, manifest, absent,
					"%s is not published, so the manifest reads it from the local build", absent)
			}
			assert.NotContains(t, manifest, "0.1.0-alpha.0",
				"a build version in a committed manifest would change with every build")
		})
	}
}

// The Go module reads the SDK this repository generates, and the local feed is where the .NET
// build reads the package the repository built.
func TestTheManifestsReachTheLocalBuild(t *testing.T) {
	t.Parallel()
	assert.Contains(t, readProgram(t, filepath.Join("go", "go.mod")),
		"=> ../../sdk/go/mailslurp")
	assert.Contains(t, readProgram(t, filepath.Join("dotnet", "mailslurp-base.csproj")),
		"<RestoreAdditionalProjectSources>$(PULUMI_LOCAL_NUGET)</RestoreAdditionalProjectSources>")
}

// The update program differs from the base program in the template content and nothing else. Any
// other difference would make the update leg prove something it does not claim.
func TestTheUpdateProgramChangesTheTemplateContentAlone(t *testing.T) {
	t.Parallel()
	const before = "Your inbox {{inboxAddress}} is ready."
	const after = "Your inbox {{inboxAddress}} is ready. Reply to {{replyTo}}."

	base := readProgram(t, filepath.Join(baseSource, "Pulumi.yaml"))
	update := readProgram(t, filepath.Join("base-update", "Pulumi.yaml"))
	require.Contains(t, base, before)
	assert.Equal(t, strings.Replace(base, before, after, 1), update,
		"the update program is the base program with one line of template content changed")
}

// The inbox update program changes the description alone, which is a property that forces no
// replacement, so the leg pays for one inbox rather than two.
func TestTheInboxUpdateProgramChangesTheDescriptionAlone(t *testing.T) {
	t.Parallel()
	const before = "description: The inbox this program creates"
	const after = "description: The inbox this program updated"

	program := readProgram(t, filepath.Join("inbox", "Pulumi.yaml"))
	update := readProgram(t, filepath.Join("inbox-update", "Pulumi.yaml"))
	require.Contains(t, program, before)
	assert.Equal(t, strings.Replace(program, before, after, 1), update)
}

// The empty preview and update after the first one is what sends the provider the inputs the
// engine holds. Turning any of these steps off would leave that path unexercised.
func TestTheProgramTestsKeepTheSecondPreviewAndTheRefresh(t *testing.T) {
	t.Parallel()
	builders := map[string]func(*testing.T) integration.ProgramTestOptions{
		"base": baseOptions, "nodejs": nodejsOptions, "python": pythonOptions,
		"dotnet": dotnetOptions, "go": goOptions,
	}
	for language, build := range builders {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			requireTheFullLifecycle(t, build(t))
		})
	}
}

// The converter reports an unknown resource on the error stream and still exits with the code 0,
// so a guard that read the exit code alone would write a program with that resource missing.
func TestTheConversionRefusesAProgramTheConverterReportsOn(t *testing.T) {
	t.Parallel()
	broken := strings.Replace(readProgram(t, filepath.Join(baseSource, "Pulumi.yaml")),
		"mailslurp:index:Webhook", "mailslurp:index:Nowhere", 1)

	answered, err := runConversion(t, seedConversionSource(t, broken), t.TempDir())
	require.Errorf(t, err, "the conversion should refuse this program: %s", answered)
	assert.Contains(t, answered, "reported an error", "the guard should name what it refused")
	assert.Contains(t, answered, `unable to find resource type "mailslurp:index:Nowhere"`,
		"the refusal should be the unknown resource, not a provider the conversion could not load")
}

// A source with no program at all makes the converter write a lowercase diagnostic and exit with
// a non-zero status, so the status is what refuses it rather than the error stream.
func TestTheConversionRefusesASourceWithNoProgram(t *testing.T) {
	t.Parallel()
	answered, err := runConversion(t, t.TempDir(), t.TempDir())
	require.Errorf(t, err, "the conversion should refuse an empty directory: %s", answered)
	assert.Contains(t, answered, "failed with the status", "the guard should name the status")
}

// A stale file of an earlier conversion reads exactly like a fresh one, so the destination starts
// empty. A program that lost a resource would otherwise keep the copy that still held it.
func TestTheConversionEmptiesTheDestinationFirst(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	stale := filepath.Join(out, "stale.ts")
	require.NoError(t, os.WriteFile(stale, []byte("// left behind\n"), 0o600))

	source := seedConversionSource(t, readProgram(t, filepath.Join(baseSource, "Pulumi.yaml")))
	answered, err := runConversion(t, source, out)
	require.NoErrorf(t, err, "the conversion should succeed: %s", answered)

	_, err = os.Stat(stale)
	assert.Truef(t, os.IsNotExist(err), "the stale file should be gone, and the error is %v", err)
}

// seedConversionSource writes one program into a directory of its own, and points it at the
// provider binary this repository built so the conversion needs no installed plugin.
func seedConversionSource(t *testing.T, program string) string {
	t.Helper()
	binPath, err := filepath.Abs("../bin")
	require.NoError(t, err)

	source := t.TempDir()
	program += "\nplugins:\n  providers:\n    - name: mailslurp\n      path: " + binPath + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(source, "Pulumi.yaml"), []byte(program), 0o600))
	return source
}

func runConversion(t *testing.T, source, out string) (string, error) {
	t.Helper()
	return runConversionInto(t, "nodejs", source, out)
}

func runConversionInto(t *testing.T, language, source, out string) (string, error) {
	t.Helper()
	//nolint:gosec // G204: the language is a constant here and both directories are the ones
	// this test just made.
	command := exec.Command("./convert.sh", language, source, out)
	answered, err := command.CombinedOutput()
	return string(answered), err
}

// The conversion takes a destination, and the Go leg rebuilt the module manifest with a replace
// written relative to that destination. Anywhere but examples/go, the SDK resolved nowhere.
func TestTheGoConversionResolvesTheSDKFromAnyDestination(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "go")
	source := seedConversionSource(t, readProgram(t, filepath.Join(baseSource, "Pulumi.yaml")))

	answered, err := runConversionInto(t, "go", source, out)
	require.NoErrorf(t, err, "the conversion should succeed: %s", answered)

	manifest := readProgram(t, filepath.Join(out, "go.mod"))
	assert.Contains(t, manifest, "=> ../../sdk/go/mailslurp",
		"the committed manifest reads the SDK by the path examples/go sits at")
}

// The count of a deployed stack is what the budget is charged from, so a count that read the
// wrong resource type would let a billed inbox through unrecorded.
func TestTheInboxCountOfADeployedStackIsRead(t *testing.T) {
	t.Parallel()
	stack := integration.RuntimeValidationStackInfo{Deployment: &apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{Type: "mailslurp:index:InboxRuleset"},
			{Type: inboxResourceType},
			{Type: "mailslurp:index:InboxForwarder"},
			{Type: inboxResourceType},
			{Type: "pulumi:pulumi:Stack"},
		},
	}}
	assert.EqualValues(t, 2, countInboxResources(stack),
		"the ruleset and the forwarder share the first word of the inbox type")
	assert.Zero(t, countInboxResources(integration.RuntimeValidationStackInfo{}),
		"a stack the export could not read holds no inbox to charge")
}

// The Makefile publishes a passphrase for the local state, and the state of a credentialed run
// holds the API key of the provider. This run encrypts that state with a passphrase of its own.
func TestTheSuiteEncryptsTheStateWithAPassphraseOfItsOwn(t *testing.T) {
	t.Parallel()
	passphrase := os.Getenv(passphraseVariable)
	assert.Len(t, passphrase, 32, "the suite sets a passphrase of sixteen random bytes")
	assert.NotEqual(t, "ci-local-passphrase", passphrase,
		"the published passphrase would let any reader of the state recover the API key")
	assert.NotEqual(t, newRunPassphrase(), passphrase, "each call answers a new passphrase")
}

// The names this package builds are the ones the sweeps of the provider tests match, so an object
// a failed run leaves behind is still removed.
func TestTheObjectNamesCarryTheSweepMarker(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{inboxKind, webhookKind, rulesetKind, templateKind, forwarderKind} {
		name := newTestName(kind)
		assert.Regexpf(t, `^pulumi-test-`+kind+`-[0-9a-f]{8}$`, name, "the %s name", kind)
	}
	assert.Regexp(t, `^\*@pulumi-test-ruleset-[0-9a-f]{8}\.example\.com$`,
		rulesetTargetFor(newTestName(rulesetKind)))
	assert.Regexp(t, `^pulumi-test-forwarder-[0-9a-f]{8}@example\.com$`,
		forwarderRecipientFor(newTestName(forwarderKind)))
}

// A program that declares an inbox costs money, so the count is read before the program runs.
func TestTheInboxCountIsReadFromTheProgram(t *testing.T) {
	t.Parallel()
	assert.EqualValues(t, 0, countInboxDeclarations(t, baseSource),
		"the base program attaches to an inbox it does not create")
	assert.EqualValues(t, 1, countInboxDeclarations(t, "inbox"))
	assert.EqualValues(t, 1, countInboxDeclarations(t, completeSource))
}

// upgradeProgram is the program both providers deploy. It creates no inbox, so an upgrade run
// costs nothing, and it carries no secret, so a preview difference is the upgrade itself.
const upgradeProgram = "upgrade"

// The upgrade program creates no inbox, so a run of it is free. A resource added to it later that
// bills would spend the budget of the whole suite on a leg nobody watches.
func TestTheUpgradeProgramCreatesNoInbox(t *testing.T) {
	assert.EqualValues(t, 0, countInboxDeclarations(t, upgradeProgram))
}
