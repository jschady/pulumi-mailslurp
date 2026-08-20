//go:build integration

package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// missingEntityID is a well-formed identifier that no MailSlurp account holds.
const missingEntityID = "00000000-0000-0000-0000-000000000000"

// listDomains reads the account domain list. The Client interface carries no list method by
// design, so this test builds its own request rather than widening that surface.
func listDomains(ctx context.Context, key string) ([]domainSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultBaseURL+domainsPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(apiKeyHeader, key)

	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the domain list answered status %d", resp.StatusCode)
	}

	var found []domainSummary
	if err := json.NewDecoder(resp.Body).Decode(&found); err != nil {
		return nil, err
	}
	return found, nil
}

// accountDomainsOrSkip answers the domains the account holds. It skips the calling test only when
// the account holds none, and prints the reason so a run without -v still reports the skip.
func accountDomainsOrSkip(t *testing.T, key string) []domainSummary {
	t.Helper()
	found, err := listDomains(context.Background(), key)
	require.NoError(t, err)
	if reason := skipReasonForDomains(found); reason != "" {
		fmt.Fprintf(os.Stderr, "SKIP %s: %s\n", t.Name(), reason)
		t.Skip(reason)
	}
	return found
}

func invokeFunction(t *testing.T, s integration.Server, name string, args property.Map) property.Map {
	t.Helper()
	resp, err := s.Invoke(p.InvokeRequest{Token: tokens.Type(name), Args: args})
	require.NoError(t, err)
	require.Empty(t, resp.Failures)
	return resp.Return
}

// The two functions read, so the account must hold exactly what it held before the reads. The
// counts reach stderr, so a run reports the before and after state without -v.
func TestNeitherFunctionChangesTheAccount(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	guardTheInboxBudget(t)

	before := readAccount(t, key)
	fmt.Fprintf(os.Stderr, "before the function reads: %d domains, %d inboxes, %d webhooks\n",
		len(before.Domains), len(before.Inboxes), len(before.Webhooks))

	s := configuredServer(t, key)
	invokeFunction(t, s, getInboxFunction, property.NewMap(map[string]property.Value{
		inboxIDProperty: property.New(sharedInboxID),
	}))
	for _, id := range before.Domains {
		invokeFunction(t, s, getDomainFunction, property.NewMap(map[string]property.Value{
			domainIDProperty: property.New(id),
			"checkForErrors": property.New(true),
		}))
	}

	after := readAccount(t, key)
	fmt.Fprintf(os.Stderr, "after the function reads: %d domains, %d inboxes, %d webhooks\n",
		len(after.Domains), len(after.Inboxes), len(after.Webhooks))
	assert.Equal(t, before, after, "a read-only function changed the account")
}

// Reading a domain costs nothing, and this test creates nothing. It reads every domain the account
// holds, so it grows with the account instead of naming a fixed domain.
func TestGetDomainReadsTheDomainsTheAccountAlreadyHolds(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)
	before := accountDomainsOrSkip(t, key)

	s := configuredServer(t, key)
	for _, summary := range before {
		result := invokeFunction(t, s, getDomainFunction, property.NewMap(map[string]property.Value{
			domainIDProperty: property.New(summary.ID),
		}))

		assert.Equal(t, summary.ID, result.Get(domainIDProperty).AsString())
		assert.Equal(t, summary.Domain, result.Get("domain").AsString())
		assert.Contains(t, domainTypeValues, result.Get("domainType").AsString())
		assert.NotEmpty(t, result.Get("verificationToken").AsString())
		assert.NotEmpty(t, result.Get("dkimTokens").AsArray().AsSlice())
		assert.NotEmpty(t, result.Get("createdAt").AsString())

		records := result.Get("domainNameRecords").AsArray()
		require.NotZero(t, records.Len(), "%s carries no DNS record", summary.Domain)
		for i := range records.Len() {
			record := records.Get(i).AsMap()
			for _, name := range domainRecordProperties {
				_, ok := record.GetOk(name)
				assert.True(t, ok, "%s record %d drops %q", summary.Domain, i, name)
			}
			assert.NotEmpty(t, record.Get("label").AsString())
			assert.NotEmpty(t, record.Get("recordType").AsString())
			assert.NotEmpty(t, record.Get(recordNameProp).AsString())
			assert.NotEmpty(t, record.Get("recordEntries").AsArray().AsSlice())
			assert.Positive(t, record.Get("ttl").AsNumber())
		}
	}

	after, err := listDomains(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the reads changed the account domains")
}

// `checkForErrors` is optional on getDomain. The live call must answer the same domain either way.
func TestGetDomainAcceptsCheckForErrorsAgainstALiveDomain(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)
	domains := accountDomainsOrSkip(t, key)

	s := configuredServer(t, key)
	plain := invokeFunction(t, s, getDomainFunction, property.NewMap(map[string]property.Value{
		domainIDProperty: property.New(domains[0].ID),
	}))
	checked := invokeFunction(t, s, getDomainFunction, property.NewMap(map[string]property.Value{
		domainIDProperty: property.New(domains[0].ID),
		"checkForErrors": property.New(true),
	}))

	assert.Equal(t, plain.Get(domainIDProperty).AsString(), checked.Get(domainIDProperty).AsString())
	assert.Equal(t, plain.Get("isVerified").AsBool(), checked.Get("isVerified").AsBool())
}

// The shared harness inbox belongs to the suite, so this read spends no budget of its own. It
// replaces the catch-all inbox of a verified domain, which this account does not hold.
func TestGetInboxReadsAnInboxTheAccountAlreadyHolds(t *testing.T) {
	key := requireAPIKey(t)
	sharedInboxID := theSharedInbox(t)
	guardTheInboxBudget(t)

	// The client read is the oracle. The account plan decides values such as `virtualInbox`, so the
	// test compares the function against the API answer instead of naming a value.
	client, err := NewClient(defaultBaseURL, key)
	require.NoError(t, err)
	dto, err := client.GetInbox(context.Background(), sharedInboxID)
	require.NoError(t, err)

	s := configuredServer(t, key)
	result := invokeFunction(t, s, getInboxFunction, property.NewMap(map[string]property.Value{
		inboxIDProperty: property.New(sharedInboxID),
	}))

	assert.Equal(t, sharedInboxID, result.Get(inboxIDProperty).AsString())
	assert.Equal(t, dto.EmailAddress, result.Get("emailAddress").AsString())
	assert.Contains(t, result.Get("emailAddress").AsString(), "@")
	assert.Equal(t, dto.CreatedAt, result.Get("createdAt").AsString())
	assert.Equal(t, dto.Favourite, result.Get("favourite").AsBool())
	assert.Equal(t, dto.ReadOnly, result.Get("readOnly").AsBool())
	assert.Equal(t, dto.VirtualInbox, result.Get("virtualInbox").AsBool())

	_, err = time.Parse(time.RFC3339, result.Get("createdAt").AsString())
	assert.NoError(t, err, "the creation time must parse as RFC 3339: %q", dto.CreatedAt)

	name, ok := result.GetOk("name")
	require.True(t, ok, "the inbox carries a name, so the result must reach the stack with it")
	assert.Equal(t, theSharedInboxName(t), name.AsString())

	// The shared inbox carries a tag, so the live list proves the tags reach the stack.
	var tags []string
	for _, value := range result.Get("tags").AsArray().AsSlice() {
		tags = append(tags, value.AsString())
	}
	assert.Equal(t, dto.Tags, tags)
	assert.Contains(t, tags, testNamePrefix+"shared")

	inboxType, present := result.GetOk("inboxType")
	assert.Equal(t, text(dto.InboxType) != nil, present, "the result must follow the API on inboxType")
	if present {
		assert.Contains(t, []string{string(InboxTypeHTTP), string(InboxTypeSMTP)}, inboxType.AsString())
		assert.Equal(t, *dto.InboxType, inboxType.AsString())
	}
}

// The live 404 carries the W_404_ENTITY_NOT_FOUND code, which both functions turn into a
// diagnostic that names the value to check.
func TestBothFunctionsReportTheActionableErrorAgainstTheLiveAPI(t *testing.T) {
	key := requireAPIKey(t)
	guardTheInboxBudget(t)
	s := configuredServer(t, key)

	_, err := s.Invoke(p.InvokeRequest{
		Token: tokens.Type(getInboxFunction),
		Args:  property.NewMap(map[string]property.Value{inboxIDProperty: property.New(missingEntityID)}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MailSlurp has no inbox with the identifier")

	_, err = s.Invoke(p.InvokeRequest{
		Token: tokens.Type(getDomainFunction),
		Args:  property.NewMap(map[string]property.Value{domainIDProperty: property.New(missingEntityID)}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MailSlurp has no domain with the identifier")
}
