package internal

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestDomainDetail = `{
  "id": "` + clientTestDomainID + `",
  "domain": "mail.example.com",
  "verificationToken": "ms-verify-8ce3bbb3",
  "dkimTokens": ["dkim1", "dkim2", "dkim3"],
  "hasDuplicateRecords": false,
  "missingRecordsMessage": null,
  "hasMissingRecords": false,
  "isVerified": true,
  "domainNameRecords": [
    {
      "label": "VERIFICATION",
      "required": true,
      "recordType": "TXT",
      "name": "mail.example.com",
      "recordEntries": ["ms-verify-8ce3bbb3"],
      "ttl": 3600,
      "alternativeRecordEntries": []
    }
  ],
  "catchAllInboxId": null,
  "createdAt": "2026-08-18T15:24:48.111Z",
  "updatedAt": "2026-08-18T15:24:48.111Z",
  "domainType": "HTTP_INBOX"
}`

func TestClientGetDomainSendsCheckForErrorsOnlyWhenAsked(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestDomainDetail))

	_, err := c.GetDomain(t.Context(), clientTestDomainID, true)
	require.NoError(t, err)

	got := rec.at(t, 0)
	require.Equal(t, http.MethodGet, got.Method)
	require.Equal(t, "/domains/"+clientTestDomainID, got.Path)
	require.Equal(t, "true", got.Query.Get("checkForErrors"))

	_, err = c.GetDomain(t.Context(), clientTestDomainID, false)
	require.NoError(t, err)
	require.NotContains(t, rec.at(t, 1).Query, "checkForErrors")
}

func TestClientGetDomainDecodesTheRecordsAThirdPartyProviderNeeds(t *testing.T) {
	c, _ := clientTestServer(t, clientTestReply(http.StatusOK, clientTestDomainDetail))

	domain, err := c.GetDomain(t.Context(), clientTestDomainID, false)
	require.NoError(t, err)

	require.Equal(t, "mail.example.com", domain.Domain)
	require.Equal(t, "ms-verify-8ce3bbb3", domain.VerificationToken)
	require.Equal(t, []string{"dkim1", "dkim2", "dkim3"}, domain.DkimTokens)
	require.True(t, domain.IsVerified)
	require.Equal(t, "HTTP_INBOX", domain.DomainType)
	require.Nil(t, domain.CatchAllInboxID)

	require.Len(t, domain.DomainNameRecords, 1)
	record := domain.DomainNameRecords[0]
	require.Equal(t, "VERIFICATION", record.Label)
	require.True(t, record.Required)
	require.Equal(t, "TXT", record.RecordType)
	require.Equal(t, "mail.example.com", record.Name)
	require.Equal(t, []string{"ms-verify-8ce3bbb3"}, record.RecordEntries)
	require.Equal(t, 3600, record.TTL)
	require.Empty(t, record.AlternativeRecordEntries)
}

func TestClientGetDomainReportsAGoneDomain(t *testing.T) {
	c, _ := clientTestServer(t, clientTestReply(http.StatusNotFound, clientTestNotFoundBody))

	_, err := c.GetDomain(t.Context(), clientTestDomainID, false)
	require.True(t, IsGone(err))
}
