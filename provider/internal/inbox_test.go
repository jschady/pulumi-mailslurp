package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const (
	inboxResourceType = "mailslurp:index:Inbox"
	testInboxID       = "inbox-1"
	testAddress       = "a@b.c"
)

func inboxURN() presource.URN {
	return presource.NewURN("test", "provider", "", tokens.Type(inboxResourceType), "test")
}

// inboxResource builds the resource with a mock client already in place, which is what Configure
// does in production.
func inboxResource(t *testing.T) (*Inbox, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &Inbox{config: &Config{client: mock}}, mock
}

// priorState is the stored state of an inbox the tests diff against. Each case overrides the one
// property it exercises.
func priorState(over map[string]property.Value) property.Map {
	m := map[string]property.Value{
		"emailAddress": property.New("prior@mailslurp.net"),
		"createdAt":    property.New("2026-08-18T00:00:00.000Z"),
		"readOnly":     property.New(false),
	}
	for k, v := range over {
		m[k] = v
	}
	return property.NewMap(m)
}

func one(key string, value property.Value) map[string]property.Value {
	return map[string]property.Value{key: value}
}

func strs(values ...string) property.Value {
	out := make([]property.Value, 0, len(values))
	for _, v := range values {
		out = append(out, property.New(v))
	}
	return property.New(out)
}

// diffInbox runs a diff through the provider server, which is the path that proves the framework
// routes to the resource's own Diff.
func diffInbox(t *testing.T, state, inputs property.Map) (p.DiffResponse, error) {
	t.Helper()
	s := newTestServer(t, nil)
	return s.Diff(p.DiffRequest{
		ID:     testInboxID,
		Urn:    inboxURN(),
		State:  state,
		Inputs: inputs,
	})
}

// The nine create-only properties of the Inbox, each with a prior value and a changed value.
type propertyCase struct {
	property string
	prior    property.Value
	changed  property.Value
}

func replaceForcingCases() []propertyCase {
	return []propertyCase{
		{inboxPropEmailAddress, property.New("prior@mailslurp.net"), property.New("next@mailslurp.net")},
		{inboxPropDomainName, property.New("old.example.com"), property.New("new.example.com")},
		{inboxPropDomainID, property.New("domain-old"), property.New("domain-new")},
		{inboxPropInboxType, property.New("HTTP_INBOX"), property.New("SMTP_INBOX")},
		{inboxPropUseDomainPool, property.New(false), property.New(true)},
		{inboxPropUseShortAddress, property.New(false), property.New(true)},
		{inboxPropVirtualInbox, property.New(false), property.New(true)},
		{inboxPropPrefix, property.New("old"), property.New("new")},
		{inboxPropExpiresIn, property.New(float64(1000)), property.New(float64(2000))},
	}
}

func updatableCases() []propertyCase {
	return []propertyCase{
		{inboxPropName, property.New("old name"), property.New("new name")},
		{inboxPropDescription, property.New("old description"), property.New("new description")},
		{inboxPropTags, strs("old"), strs("new")},
		{inboxPropExpiresAt, property.New("2026-09-01T00:00:00.000Z"), property.New("2027-09-01T00:00:00.000Z")},
		{inboxPropFavourite, property.New(false), property.New(true)},
	}
}

func TestInboxDiffReplacesOnEveryCreateOnlyProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range replaceForcingCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffInbox(t,
				priorState(one(tc.property, tc.prior)),
				property.NewMap(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges, "a changed %s must produce a diff", tc.property)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.UpdateReplace, got.Kind)
		})
	}
}

func TestInboxDiffAddReplacesWhenACreateOnlyPropertyAppears(t *testing.T) {
	t.Parallel()
	for _, tc := range replaceForcingCases() {
		if tc.property == inboxPropEmailAddress {
			// The state always carries the assigned address, so it is never absent.
			continue
		}
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffInbox(t, priorState(nil), property.NewMap(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.AddReplace, got.Kind)
		})
	}
}

func TestInboxDiffUpdatesInPlaceOnEveryUpdatableProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range updatableCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffInbox(t,
				priorState(one(tc.property, tc.prior)),
				property.NewMap(one(tc.property, tc.changed)))
			require.NoError(t, err)
			assert.True(t, resp.HasChanges)
			got, ok := resp.DetailedDiff[tc.property]
			require.True(t, ok, "no detailed diff for %s", tc.property)
			assert.Equal(t, p.Update, got.Kind, "%s must update in place", tc.property)
		})
	}
}

// An unset create-only input means the program states no opinion. Reading it as a removal would
// destroy the inbox and bill a new one, so it must never produce a replace.
func TestInboxDiffNeverReplacesWhenACreateOnlyInputIsUnset(t *testing.T) {
	t.Parallel()
	for _, tc := range replaceForcingCases() {
		t.Run(tc.property, func(t *testing.T) {
			t.Parallel()
			resp, err := diffInbox(t, priorState(one(tc.property, tc.prior)), property.NewMap(nil))
			require.NoError(t, err)
			assert.False(t, resp.HasChanges, "an unset %s must not diff", tc.property)
			assert.Empty(t, resp.DetailedDiff)
		})
	}
}

func TestInboxDiffReportsNoChangesWhenTheProgramMatchesTheState(t *testing.T) {
	t.Parallel()
	inputs := map[string]property.Value{}
	state := map[string]property.Value{}
	for _, tc := range append(replaceForcingCases(), updatableCases()...) {
		inputs[tc.property] = tc.prior
		state[tc.property] = tc.prior
	}
	resp, err := diffInbox(t, priorState(state), property.NewMap(inputs))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
	assert.Empty(t, resp.DetailedDiff)
}

// PATCH swallows an explicit null and answers 200, so a set-to-unset transition on a
// string property can never converge. The diff refuses it instead of reporting a false success.
func TestInboxDiffRefusesToClearAPropertyTheAPICannotClear(t *testing.T) {
	t.Parallel()
	for _, name := range []string{inboxPropName, inboxPropDescription, inboxPropExpiresAt} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := diffInbox(t,
				priorState(one(name, property.New("a value"))),
				property.NewMap(nil))
			require.Error(t, err, "clearing %s must fail loudly", name)
			assert.Contains(t, err.Error(), name)
			assert.Contains(t, err.Error(), "cannot clear")
		})
	}
}

// The empty list clears the tags and the API honours it, so this transition is a plain update.
func TestInboxDiffClearsTheTagsInPlace(t *testing.T) {
	t.Parallel()
	resp, err := diffInbox(t, priorState(one(inboxPropTags, strs("old"))), property.NewMap(nil))
	require.NoError(t, err)
	assert.True(t, resp.HasChanges)
	require.Contains(t, resp.DetailedDiff, inboxPropTags)
	assert.Equal(t, p.Update, resp.DetailedDiff[inboxPropTags].Kind)
}

func TestInboxDiffTreatsAnUnsetFavouriteAsFalse(t *testing.T) {
	t.Parallel()
	resp, err := diffInbox(t, priorState(one(inboxPropFavourite, property.New(false))), property.NewMap(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges, "an unset favourite matches the stored false")

	resp, err = diffInbox(t, priorState(one(inboxPropFavourite, property.New(true))), property.NewMap(nil))
	require.NoError(t, err)
	assert.True(t, resp.HasChanges)
	require.Contains(t, resp.DetailedDiff, inboxPropFavourite)
	assert.Equal(t, p.Update, resp.DetailedDiff[inboxPropFavourite].Kind)
}

func TestInboxDiffTreatsAnUnsetTagListAsEmpty(t *testing.T) {
	t.Parallel()
	resp, err := diffInbox(t, priorState(one(inboxPropTags, property.New([]property.Value{}))), property.NewMap(nil))
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
}

func TestInboxDiffCallsNoAPIMethod(t *testing.T) {
	t.Parallel()
	// A mock with no expectation fails the test on any call.
	inbox, _ := inboxResource(t)
	resp, err := inbox.Diff(context.Background(), infer.DiffRequest[InboxArgs, InboxState]{
		ID:     testInboxID,
		State:  InboxState{Name: ptr("old"), EmailAddress: "prior@mailslurp.net"},
		Inputs: InboxArgs{Name: ptr("new")},
	})
	require.NoError(t, err)
	assert.True(t, resp.HasChanges)
}

func TestInboxCreateSendsEveryInputAndReturnsTheEmailAddress(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	var got CreateInboxOptions
	mock.EXPECT().CreateInbox(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts CreateInboxOptions) (*InboxDto, error) {
			got = opts
			return &InboxDto{
				ID:           testInboxID,
				EmailAddress: "assigned@mailslurp.net",
				CreatedAt:    "2026-08-18T00:00:00.000Z",
				Name:         opts.Name,
				InboxType:    ptr("HTTP_INBOX"),
			}, nil
		})

	resp, err := inbox.Create(context.Background(), infer.CreateRequest[InboxArgs]{
		Name: "test",
		Inputs: InboxArgs{
			Name:            ptr("pulumi-test-inbox-abcd1234"),
			Description:     ptr("a description"),
			Tags:            []string{"a", "b"},
			ExpiresAt:       ptr("2027-01-01T00:00:00.000Z"),
			Favourite:       ptr(true),
			EmailAddress:    ptr("pinned@mailslurp.net"),
			DomainName:      ptr("example.com"),
			DomainID:        ptr("domain-1"),
			InboxType:       ptr(InboxTypeHTTP),
			UseDomainPool:   ptr(true),
			UseShortAddress: ptr(true),
			VirtualInbox:    ptr(true),
			Prefix:          ptr("pre"),
			ExpiresIn:       ptr(int64(60000)),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, testInboxID, resp.ID)
	assert.Equal(t, "assigned@mailslurp.net", resp.Output.EmailAddress)

	assert.Equal(t, "pulumi-test-inbox-abcd1234", *got.Name)
	assert.Equal(t, "a description", *got.Description)
	assert.Equal(t, []string{"a", "b"}, got.Tags)
	assert.Equal(t, "2027-01-01T00:00:00.000Z", *got.ExpiresAt)
	assert.True(t, *got.Favourite)
	assert.Equal(t, "pinned@mailslurp.net", *got.EmailAddress)
	assert.Equal(t, "example.com", *got.DomainName)
	assert.Equal(t, "domain-1", *got.DomainID)
	assert.Equal(t, "HTTP_INBOX", *got.InboxType)
	assert.True(t, *got.UseDomainPool)
	assert.True(t, *got.UseShortAddress)
	assert.True(t, *got.VirtualInbox)
	assert.Equal(t, "pre", *got.Prefix)
	assert.Equal(t, int64(60000), *got.ExpiresIn)
}

// The write-only properties never come back from the API, so create stores what the program set.
func TestInboxCreateCarriesTheWriteOnlyPropertiesIntoTheState(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().CreateInbox(gomock.Any(), gomock.Any()).
		Return(&InboxDto{ID: testInboxID, EmailAddress: testAddress}, nil)

	resp, err := inbox.Create(context.Background(), infer.CreateRequest[InboxArgs]{
		Inputs: InboxArgs{
			UseDomainPool:   ptr(true),
			UseShortAddress: ptr(true),
			Prefix:          ptr("pre"),
			ExpiresIn:       ptr(int64(60000)),
			DomainName:      ptr("example.com"),
			DomainID:        ptr("domain-1"),
		},
	})
	require.NoError(t, err)
	assert.True(t, *resp.Output.UseDomainPool)
	assert.True(t, *resp.Output.UseShortAddress)
	assert.Equal(t, "pre", *resp.Output.Prefix)
	assert.Equal(t, int64(60000), *resp.Output.ExpiresIn)
	assert.Equal(t, "example.com", *resp.Output.DomainName)
	assert.Equal(t, "domain-1", *resp.Output.DomainID)
}

// A preview must not spend a billed inbox. LifeCycleTest previews before every create.
func TestInboxCreateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	inbox, _ := inboxResource(t)
	resp, err := inbox.Create(context.Background(), infer.CreateRequest[InboxArgs]{
		Inputs: InboxArgs{Name: ptr("preview")},
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "a preview must not claim an id")
}

// A provider that never ran Configure carries no configuration, and a Configure that failed can
// leave a configuration without a client. The create must refuse both.
func TestInboxCreateFailsWhenTheProviderIsNotConfigured(t *testing.T) {
	t.Parallel()
	for title, inbox := range map[string]*Inbox{
		titleWithoutConfiguration: {},
		titleWithoutClient:        {config: &Config{}},
	} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			_, err := inbox.Create(context.Background(), infer.CreateRequest[InboxArgs]{})
			requireNotConfigured(t, err)
		})
	}
}

func TestInboxReadOnAMissingInboxReturnsAnEmptyResponse(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	resp, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{
		ID:    testInboxID,
		State: InboxState{EmailAddress: testAddress},
	})
	require.NoError(t, err, "a deleted inbox is drift, not a failure")
	assert.Empty(t, resp.ID, "an empty id tells the engine the inbox is gone")
}

// An unrouted path answers 404 too. Only the documented error code means the inbox is gone.
func TestInboxReadReturnsTheErrorForAnyOtherFailure(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
	})

	_, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{ID: testInboxID})
	require.Error(t, err)
}

func TestInboxReadKeepsThePriorInboxTypeWhenTheAPIAnswersNull(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).
		Return(&InboxDto{ID: testInboxID, EmailAddress: testAddress, InboxType: nil}, nil)

	resp, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{
		ID:     testInboxID,
		Inputs: InboxArgs{InboxType: ptr(InboxTypeSMTP)},
		State:  InboxState{EmailAddress: testAddress, InboxType: ptr(InboxTypeSMTP)},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.State.InboxType)
	assert.Equal(t, InboxTypeSMTP, *resp.State.InboxType)
}

func TestInboxReadTakesTheInboxTypeWhenTheAPIReportsOne(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).
		Return(&InboxDto{ID: testInboxID, EmailAddress: testAddress, InboxType: ptr("SMTP_INBOX")}, nil)

	resp, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{
		ID:    testInboxID,
		State: InboxState{EmailAddress: testAddress, InboxType: ptr(InboxTypeHTTP)},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.State.InboxType)
	assert.Equal(t, InboxTypeSMTP, *resp.State.InboxType)
}

// The six carried properties are absent from every read, so a refresh carries them forward from
// the inputs. A refresh that dropped them would plan to remove what the program set.
func TestInboxReadCarriesTheWriteOnlyPropertiesFromThePriorState(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).
		Return(&InboxDto{ID: testInboxID, EmailAddress: testAddress}, nil)

	prior := InboxState{
		EmailAddress:    testAddress,
		UseDomainPool:   ptr(true),
		UseShortAddress: ptr(true),
		Prefix:          ptr("pre"),
		ExpiresIn:       ptr(int64(60000)),
		DomainName:      ptr("example.com"),
		DomainID:        ptr("domain-1"),
	}
	resp, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{
		ID: testInboxID, State: prior,
	})
	require.NoError(t, err)
	assert.True(t, *resp.State.UseDomainPool)
	assert.True(t, *resp.State.UseShortAddress)
	assert.Equal(t, "pre", *resp.State.Prefix)
	assert.Equal(t, int64(60000), *resp.State.ExpiresIn)
	assert.Equal(t, "example.com", *resp.State.DomainName)
	assert.Equal(t, "domain-1", *resp.State.DomainID)

	assert.True(t, *resp.Inputs.UseDomainPool)
	assert.True(t, *resp.Inputs.UseShortAddress)
	assert.Equal(t, "pre", *resp.Inputs.Prefix)
	assert.Equal(t, int64(60000), *resp.Inputs.ExpiresIn)
	assert.Equal(t, "example.com", *resp.Inputs.DomainName)
	assert.Equal(t, "domain-1", *resp.Inputs.DomainID)
}

func TestInboxReadRefreshesTheUpdatableProperties(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(&InboxDto{
		ID:           testInboxID,
		EmailAddress: testAddress,
		Name:         ptr("renamed on the server"),
		Description:  ptr("changed"),
		Tags:         []string{"x"},
		Favourite:    true,
	}, nil)

	resp, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{
		ID:    testInboxID,
		State: InboxState{EmailAddress: testAddress, Name: ptr("old"), Tags: []string{"old"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "renamed on the server", *resp.State.Name)
	assert.Equal(t, "changed", *resp.State.Description)
	assert.Equal(t, []string{"x"}, resp.State.Tags)
	assert.True(t, *resp.State.Favourite)
}

// The live account holds inboxes whose name is the empty string. Reading that as a value would
// make the diff demand a clear that the API cannot perform.
func TestInboxReadTreatsAnEmptyNameAsUnset(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(&InboxDto{
		ID: testInboxID, EmailAddress: testAddress, Name: ptr(""), Description: ptr(""),
	}, nil)

	resp, err := inbox.Read(context.Background(), infer.ReadRequest[InboxArgs, InboxState]{
		ID: testInboxID, State: InboxState{EmailAddress: testAddress},
	})
	require.NoError(t, err)
	assert.Nil(t, resp.State.Name)
	assert.Nil(t, resp.State.Description)
}

// An import calls Read with no input and no prior state, so the API answer is the only source of
// the inputs. Without them `pulumi import` writes a program with no property, and preview fails.
func TestInboxReadAnswersTheInputsThatAnImportDoesNotCarry(t *testing.T) {
	t.Parallel()
	mock := NewMockClient(gomock.NewController(t))
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(sampleInboxDto(), nil)

	resp, err := serverWithClient(t, mock).Read(p.ReadRequest{
		Urn:        inboxURN(),
		ID:         testInboxID,
		Properties: property.NewMap(nil),
		Inputs:     property.NewMap(nil),
	})
	require.NoError(t, err)
	require.Equal(t, testInboxID, resp.ID)

	want := map[string]property.Value{
		inboxPropName:         property.New("the name"),
		inboxPropDescription:  property.New("the description"),
		inboxPropTags:         strs("alpha", "beta"),
		inboxPropExpiresAt:    property.New("2026-09-18T00:00:00.000Z"),
		inboxPropFavourite:    property.New(true),
		inboxPropEmailAddress: property.New(testAddress),
		inboxPropInboxType:    property.New(string(InboxTypeSMTP)),
		inboxPropVirtualInbox: property.New(true),
	}
	for name, value := range want {
		got, ok := resp.Inputs.GetOk(name)
		require.True(t, ok, "an import writes no %s into the program", name)
		assert.Equal(t, value, got, "the imported %s", name)
	}

	// A refresh carries these six properties forward from the prior write, because no answer of
	// the API repeats them in the shape a program writes. An import has no prior write.
	for _, name := range []string{
		inboxPropDomainName, inboxPropDomainID, inboxPropUseDomainPool,
		inboxPropUseShortAddress, inboxPropPrefix, inboxPropExpiresIn,
	} {
		_, ok := resp.Inputs.GetOk(name)
		assert.False(t, ok, "an import invents %s, which the state cannot carry", name)
	}
}

// The update sends all five updatable properties as full desired state, so whether the API
// merges the body or replaces it cannot change the code path.
func TestInboxUpdateSendsAllFiveUpdatableProperties(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	var got UpdateInboxOptions
	mock.EXPECT().UpdateInbox(gomock.Any(), testInboxID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts UpdateInboxOptions) (*InboxDto, error) {
			got = opts
			return &InboxDto{ID: testInboxID, EmailAddress: testAddress, Name: opts.Name}, nil
		})

	_, err := inbox.Update(context.Background(), infer.UpdateRequest[InboxArgs, InboxState]{
		ID:    testInboxID,
		State: InboxState{EmailAddress: testAddress, Name: ptr("old")},
		Inputs: InboxArgs{
			Name:        ptr("new"),
			Description: ptr("a description"),
			Tags:        []string{"a"},
			ExpiresAt:   ptr("2027-01-01T00:00:00.000Z"),
			Favourite:   ptr(true),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "new", *got.Name)
	assert.Equal(t, "a description", *got.Description)
	require.NotNil(t, got.Tags)
	assert.Equal(t, []string{"a"}, *got.Tags)
	assert.Equal(t, "2027-01-01T00:00:00.000Z", *got.ExpiresAt)
	assert.True(t, *got.Favourite)
}

// An explicit null returns 200 and changes nothing, so the body must never carry one.
func TestInboxUpdateNeverSendsANullProperty(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	var got UpdateInboxOptions
	mock.EXPECT().UpdateInbox(gomock.Any(), testInboxID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, opts UpdateInboxOptions) (*InboxDto, error) {
			got = opts
			return &InboxDto{ID: testInboxID, EmailAddress: testAddress}, nil
		})

	_, err := inbox.Update(context.Background(), infer.UpdateRequest[InboxArgs, InboxState]{
		ID:     testInboxID,
		State:  InboxState{EmailAddress: testAddress, Tags: []string{"old"}},
		Inputs: InboxArgs{},
	})
	require.NoError(t, err)

	body, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "null", "a null property is swallowed with a lying 200")
	assert.Contains(t, string(body), `"tags":[]`, "an empty list is how the API clears the tags")
	assert.Contains(t, string(body), `"favourite":false`)
}

func TestInboxUpdateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	inbox, _ := inboxResource(t)
	resp, err := inbox.Update(context.Background(), infer.UpdateRequest[InboxArgs, InboxState]{
		ID:     testInboxID,
		State:  InboxState{EmailAddress: testAddress},
		Inputs: InboxArgs{Name: ptr("new")},
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Equal(t, testAddress, resp.Output.EmailAddress)
}

// The create-only properties never change without a replacement, so the update leaves them alone.
func TestInboxUpdateKeepsTheCreateOnlyPropertiesFromTheState(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().UpdateInbox(gomock.Any(), testInboxID, gomock.Any()).
		Return(&InboxDto{ID: testInboxID, EmailAddress: testAddress}, nil)

	resp, err := inbox.Update(context.Background(), infer.UpdateRequest[InboxArgs, InboxState]{
		ID: testInboxID,
		State: InboxState{
			EmailAddress: testAddress,
			Prefix:       ptr("pre"),
			ExpiresIn:    ptr(int64(60000)),
			DomainName:   ptr("example.com"),
			CreatedAt:    "2026-08-18T00:00:00.000Z",
		},
		Inputs: InboxArgs{Name: ptr("new")},
	})
	require.NoError(t, err)
	assert.Equal(t, "pre", *resp.Output.Prefix)
	assert.Equal(t, int64(60000), *resp.Output.ExpiresIn)
	assert.Equal(t, "example.com", *resp.Output.DomainName)
	assert.Equal(t, "2026-08-18T00:00:00.000Z", resp.Output.CreatedAt)
}

func TestInboxDeleteRemovesTheInbox(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().DeleteInbox(gomock.Any(), testInboxID).Return(nil)

	_, err := inbox.Delete(context.Background(), infer.DeleteRequest[InboxState]{ID: testInboxID})
	require.NoError(t, err)
}

func TestInboxDeleteToleratesAnInboxThatIsAlreadyGone(t *testing.T) {
	t.Parallel()
	inbox, mock := inboxResource(t)
	mock.EXPECT().DeleteInbox(gomock.Any(), testInboxID).Return(&APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	_, err := inbox.Delete(context.Background(), infer.DeleteRequest[InboxState]{ID: testInboxID})
	require.NoError(t, err)
}

// Inbox naming and sweep policy. The integration sweeper reads these, and this file carries no
// build tag, so both tiers do.
const testInboxKind = "inbox"

// testInboxNamePattern matches only what newTestName builds. A live account can hold inboxes this
// suite never made, so an inbox that fails this pattern is never a sweep candidate.
var testInboxNamePattern = regexp.MustCompile(`^` + testNamePrefix + testInboxKind + `-[0-9a-f]{8}$`)

// isSweepableTestInbox reports whether the sweeper may delete an inbox. It answers false for
// every name this package did not build, and for a test inbox younger than sweepMinAge.
func isSweepableTestInbox(name *string, createdAt, now time.Time) bool {
	if name == nil || !testInboxNamePattern.MatchString(*name) {
		return false
	}
	return now.Sub(createdAt) > sweepMinAge
}

// The sweeper deletes inboxes from a live account, so its predicate must answer false for every
// name this package did not build.
func TestTheSweeperMatchesOnlyTheNamesTheTestsBuild(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	now := old.Add(24 * time.Hour)

	for _, name := range []string{
		"pulumi-test-inbox-abcd1234",
		"pulumi-test-inbox-00000000",
		"pulumi-test-inbox-ffffffff",
	} {
		assert.True(t, isSweepableTestInbox(&name, old, now), "must sweep %q", name)
	}

	// Every one of these is a name shape a live account can carry, or a near miss.
	for _, name := range []string{
		"", wildcardTarget, "Example Company Testing", "other-test-inbox",
		"The token inbox", "Another shared Inbox",
		"pulumi-test-inbox-ABCD1234", "pulumi-test-inbox-abcd123", "pulumi-test-inbox-abcd12345",
		"pulumi-test-inbox-", "pulumi-test-webhook-abcd1234", "x pulumi-test-inbox-abcd1234",
		"pulumi-test-inbox-abcd1234 x", "pulumi-test-inbox-abcd1234\n",
	} {
		assert.False(t, isSweepableTestInbox(&name, old, now), "must not sweep %q", name)
	}

	assert.False(t, isSweepableTestInbox(nil, old, now), "a null name is never a sweep candidate")

	fresh := now.Add(-time.Minute)
	sweepable := "pulumi-test-inbox-abcd1234"
	assert.False(t, isSweepableTestInbox(&sweepable, fresh, now),
		"a fresh inbox can belong to a concurrent run")
}

func TestNewTestNameBuildsASweepableName(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 100 {
		name := newTestName(testInboxKind)
		assert.True(t, testInboxNamePattern.MatchString(name), "%q", name)
		assert.False(t, seen[name], "newTestName repeated %q", name)
		seen[name] = true
	}
}

// The diff refuses a transition the API cannot perform, so the message must say what to do.
func TestTheClearErrorMessageStaysInsideTheWritingLimits(t *testing.T) {
	t.Parallel()
	_, err := diffInbox(t, priorState(one(inboxPropName, property.New("a value"))), property.NewMap(nil))
	require.Error(t, err)
	for _, sentence := range splitSentences(err.Error()) {
		words := strings.Fields(backticked.ReplaceAllString(sentence, "X"))
		assert.LessOrEqual(t, len(words), 20, "sentence of %d words: %s", len(words), sentence)
	}
	assert.NotContains(t, strings.ToLower(err.Error()), "please")
}

// The diff table is the sole source of the replacement kinds, so it must cover exactly the
// schema input properties, with the same nine forcing a replacement.
func TestTheDiffTableMatchesTheSchemaContract(t *testing.T) {
	t.Parallel()
	gotReplace := []string{}
	gotUpdate := []string{}
	seen := map[string]bool{}
	for _, prop := range inboxProperties() {
		require.False(t, seen[prop.name], "the table lists %s twice", prop.name)
		seen[prop.name] = true
		if prop.replace {
			gotReplace = append(gotReplace, prop.name)
			continue
		}
		gotUpdate = append(gotUpdate, prop.name)
	}
	assert.ElementsMatch(t, replaceForcing, gotReplace)
	assert.ElementsMatch(t, updatable, gotUpdate)

	schema := getSchema(t)
	inbox := schema.Resources[inboxResourceType]
	require.NotNil(t, inbox)
	for name := range inbox.InputProperties {
		assert.True(t, seen[name], "input property %q has no diff rule", name)
	}
	assert.Len(t, seen, len(inbox.InputProperties))
}

// The diff refuses to clear these three, so their descriptions must say so before a user writes
// a program that depends on clearing one.
func TestTheDescriptionsStateWhichPropertiesTheAPICannotClear(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	inbox := schema.Resources[inboxResourceType]
	require.NotNil(t, inbox)

	for _, name := range []string{inboxPropName, inboxPropDescription, inboxPropExpiresAt} {
		prop := inbox.InputProperties[name]
		require.NotNil(t, prop, "input property %q is missing", name)
		assert.Contains(t, prop.Description, "cannot clear this property",
			"property %q does not state the limit the diff enforces", name)
	}
	// The two the API can clear must not carry the warning.
	for _, name := range []string{inboxPropTags, inboxPropFavourite} {
		assert.NotContains(t, inbox.InputProperties[name].Description, "cannot clear")
	}
	assert.Contains(t, inbox.Description, "Pulumi keeps the inbox",
		"the resource description does not state what a removed create-only property does")
}
