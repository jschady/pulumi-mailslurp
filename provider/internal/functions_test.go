package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// The two function names this provider ships, and the two types they return.
const (
	getInboxFunction  = "mailslurp:index:getInbox"
	getDomainFunction = "mailslurp:index:getDomain"
	domainRecordType  = "mailslurp:index:DomainNameRecordResult"
	domainTypeEnum    = "mailslurp:index:DomainType"

	inboxIDProperty  = "inboxId"
	domainIDProperty = "domainId"
	recordNameProp   = "name"

	testDomainID = "domain-1"
)

// The seven properties of one DNS record, in the order the record type declares them.
var domainRecordProperties = []string{
	"label", "required", "recordType", recordNameProp, "recordEntries", "ttl",
	"alternativeRecordEntries",
}

// The verbatim domainType values of the API. The inbox type spells the second one SMTP_INBOX.
var domainTypeValues = []string{"HTTP_INBOX", "SMTP_DOMAIN"}

// fnObject is a schema object type: a function input, a function output, or a named type.
type fnObject struct {
	Description string                     `json:"description"`
	Properties  map[string]*schemaProperty `json:"properties"`
	Required    []string                   `json:"required"`
}

type fnSpec struct {
	Description string    `json:"description"`
	Inputs      *fnObject `json:"inputs"`
	Outputs     *fnObject `json:"outputs"`
}

type fnDoc struct {
	Functions map[string]*fnSpec   `json:"functions"`
	Types     map[string]*fnObject `json:"types"`
}

func getFunctionSchema(t *testing.T) fnDoc {
	t.Helper()
	s := newTestServer(t, nil)
	resp, err := s.GetSchema(p.GetSchemaRequest{Version: 0})
	require.NoError(t, err)
	var doc fnDoc
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &doc))
	return doc
}

// inboxFunction builds getInbox with a mock client already in place, which is what Configure does
// in production.
func inboxFunction(t *testing.T) (*GetInbox, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &GetInbox{config: &Config{client: mock}}, mock
}

func domainFunction(t *testing.T) (*GetDomain, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &GetDomain{config: &Config{client: mock}}, mock
}

func readInbox(t *testing.T, fn *GetInbox, inboxID string) (GetInboxResult, error) {
	t.Helper()
	resp, err := fn.Invoke(context.Background(), infer.FunctionRequest[GetInboxArgs]{
		Input: GetInboxArgs{InboxID: inboxID},
	})
	return resp.Output, err
}

func readDomain(t *testing.T, fn *GetDomain, args GetDomainArgs) (GetDomainResult, error) {
	t.Helper()
	resp, err := fn.Invoke(context.Background(), infer.FunctionRequest[GetDomainArgs]{Input: args})
	return resp.Output, err
}

// sampleRecord gives every property a value no other property carries, so a swapped assignment
// in the mapping fails the table below instead of passing by coincidence.
func sampleRecord() DomainNameRecord {
	return DomainNameRecord{
		Label:                    "DKIM",
		Required:                 true,
		RecordType:               "CNAME",
		Name:                     "r1._domainkey.mail.example.com",
		RecordEntries:            []string{"r1.dkim.example.net"},
		TTL:                      600,
		AlternativeRecordEntries: []string{"r1.alternative.example.net"},
	}
}

func sampleInboxDto() *InboxDto {
	return &InboxDto{
		ID:            testInboxID,
		CreatedAt:     "2026-01-02T03:04:05.000Z",
		Name:          ptr("the name"),
		Description:   ptr("the description"),
		DomainID:      ptr(testDomainID),
		EmailAddress:  testAddress,
		ExpiresAt:     ptr("2026-09-18T00:00:00.000Z"),
		Favourite:     true,
		Tags:          []string{"alpha", "beta"},
		InboxType:     ptr("SMTP_INBOX"),
		ReadOnly:      true,
		VirtualInbox:  true,
		FunctionsAs:   ptr("CATCH_ALL"),
		LocalPart:     ptr("local"),
		Domain:        ptr("mail.example.com"),
		AccountRegion: ptr("US_WEST_2"),
	}
}

func sampleDomainDto() *DomainDto {
	return &DomainDto{
		ID:                    testDomainID,
		Domain:                "mail.example.com",
		VerificationToken:     "verification-value",
		DkimTokens:            []string{"dkim-1", "dkim-2", "dkim-3"},
		DuplicateRecordsMsg:   ptr("the duplicate message"),
		HasDuplicateRecords:   true,
		MissingRecordsMessage: ptr("the missing message"),
		HasMissingRecords:     true,
		IsVerified:            true,
		DomainNameRecords:     []DomainNameRecord{sampleRecord()},
		CatchAllInboxID:       ptr(testInboxID),
		CreatedAt:             "2026-02-03T04:05:06.000Z",
		UpdatedAt:             "2026-03-04T05:06:07.000Z",
		DomainType:            string(DomainTypeSMTP),
	}
}

func TestTheFunctionInputsRequireOnlyTheIdentifier(t *testing.T) {
	t.Parallel()
	doc := getFunctionSchema(t)

	inbox := doc.Functions[getInboxFunction]
	require.NotNil(t, inbox)
	require.NotNil(t, inbox.Inputs)
	assert.Equal(t, []string{inboxIDProperty}, inbox.Inputs.Required)
	assert.Contains(t, inbox.Inputs.Properties, inboxIDProperty)

	domain := doc.Functions[getDomainFunction]
	require.NotNil(t, domain)
	require.NotNil(t, domain.Inputs)
	assert.Equal(t, []string{domainIDProperty}, domain.Inputs.Required)
	assert.Contains(t, domain.Inputs.Properties, "checkForErrors")
}

// Pulumi reserves `id` for resources, so each result echoes the identifier under its own name.
func TestEachFunctionResultEchoesTheIdentifier(t *testing.T) {
	t.Parallel()
	doc := getFunctionSchema(t)

	inbox := doc.Functions[getInboxFunction]
	require.NotNil(t, inbox)
	require.NotNil(t, inbox.Outputs)
	assert.Contains(t, inbox.Outputs.Properties, inboxIDProperty)
	assert.NotContains(t, inbox.Outputs.Properties, "id")

	domain := doc.Functions[getDomainFunction]
	require.NotNil(t, domain)
	require.NotNil(t, domain.Outputs)
	assert.Contains(t, domain.Outputs.Properties, domainIDProperty)
	assert.NotContains(t, domain.Outputs.Properties, "id")
}

// getDomain returns the DomainDto shape, and the DNS records drive a DNS provider.
func TestTheGetDomainResultCarriesEveryDomainDtoProperty(t *testing.T) {
	t.Parallel()
	doc := getFunctionSchema(t)
	domain := doc.Functions[getDomainFunction]
	require.NotNil(t, domain)
	require.NotNil(t, domain.Outputs)

	want := []string{
		domainIDProperty, "domain", "domainType", "verificationToken", "dkimTokens", "isVerified",
		"hasMissingRecords", "missingRecordsMessage", "hasDuplicateRecords",
		"duplicateRecordsMessage", "domainNameRecords", "catchAllInboxId", "createdAt", "updatedAt",
	}
	for _, name := range want {
		assert.Contains(t, domain.Outputs.Properties, name, "the result drops %q", name)
	}
	assert.Len(t, domain.Outputs.Properties, len(want))
}

func TestTheDomainNameRecordTypeCarriesTheSevenProperties(t *testing.T) {
	t.Parallel()
	doc := getFunctionSchema(t)
	record := doc.Types[domainRecordType]
	require.NotNil(t, record, "the function must ship a typed DNS record")
	assert.Len(t, record.Properties, len(domainRecordProperties))
	for _, name := range domainRecordProperties {
		assert.Contains(t, record.Properties, name, "the record drops %q", name)
	}
}

// `inboxType` and `domainType` never share a Go type, because the API spells the second
// value differently for the two concepts.
func TestTheDomainTypeEnumNeverSharesTheInboxTypeValues(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)

	domainType := schema.Types[domainTypeEnum]
	require.NotNil(t, domainType)
	assert.Equal(t, "string", domainType.Type)

	got := map[string]string{}
	for _, v := range domainType.Enum {
		got[v.Value] = v.Description
	}
	assert.Len(t, got, 2)
	for _, want := range domainTypeValues {
		assert.Contains(t, got, want)
		assert.NotEmpty(t, got[want], "enum value %q has no description", want)
	}
	assert.NotContains(t, got, "SMTP_INBOX", "SMTP_INBOX belongs to the inbox type, not the domain type")
}

func TestGetInboxMapsEveryInboxDtoProperty(t *testing.T) {
	t.Parallel()
	fn, mock := inboxFunction(t)
	dto := sampleInboxDto()
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(dto, nil).Times(1)

	got, err := readInbox(t, fn, testInboxID)
	require.NoError(t, err)

	assert.Equal(t, testInboxID, got.InboxID)
	assert.Equal(t, dto.CreatedAt, got.CreatedAt)
	assert.Equal(t, dto.Name, got.Name)
	assert.Equal(t, dto.Description, got.Description)
	assert.Equal(t, dto.DomainID, got.DomainID)
	assert.Equal(t, testAddress, got.EmailAddress)
	assert.Equal(t, dto.ExpiresAt, got.ExpiresAt)
	assert.True(t, got.Favourite)
	assert.Equal(t, dto.Tags, got.Tags)
	require.NotNil(t, got.InboxType)
	assert.Equal(t, InboxTypeSMTP, *got.InboxType)
	assert.True(t, got.ReadOnly)
	assert.True(t, got.VirtualInbox)
	assert.Equal(t, dto.FunctionsAs, got.FunctionsAs)
	assert.Equal(t, dto.LocalPart, got.LocalPart)
	assert.Equal(t, dto.Domain, got.Domain)
	assert.Equal(t, dto.AccountRegion, got.AccountRegion)
}

// The API answers a null inboxType for some inboxes, and the empty list for an inbox with no tags.
func TestGetInboxAnswersTheEmptyValuesTheAPIOmits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		inboxType *string
	}{
		{"a null inboxType", nil},
		{"an empty inboxType", ptr("")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fn, mock := inboxFunction(t)
			mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(&InboxDto{
				ID: testInboxID, EmailAddress: testAddress, InboxType: c.inboxType,
				Name: ptr(""), Description: ptr(""), DomainID: ptr(""), ExpiresAt: ptr(""),
				FunctionsAs: ptr(""), LocalPart: ptr(""), Domain: ptr(""), AccountRegion: ptr(""),
			}, nil).Times(1)

			got, err := readInbox(t, fn, testInboxID)
			require.NoError(t, err)
			// The enum allows HTTP_INBOX and SMTP_INBOX only, so an empty value must not reach it.
			assert.Nil(t, got.InboxType)
			// The API answers the empty string for every property the inbox does not carry.
			for name, value := range map[string]*string{
				inboxPropName: got.Name, inboxPropDescription: got.Description,
				inboxPropDomainID: got.DomainID, inboxPropExpiresAt: got.ExpiresAt,
				"functionsAs": got.FunctionsAs, "localPart": got.LocalPart,
				"domain": got.Domain, "accountRegion": got.AccountRegion,
			} {
				assert.Nil(t, value, "the empty string reached the stack as the %q value", name)
			}
			assert.NotNil(t, got.Tags, "a stack loops over the tags without a nil check")
			assert.Empty(t, got.Tags)
		})
	}
}

func TestGetDomainMapsTheSevenDomainNameRecordProperties(t *testing.T) {
	t.Parallel()
	fn, mock := domainFunction(t)
	mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).Return(sampleDomainDto(), nil).Times(1)

	result, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
	require.NoError(t, err)
	require.Len(t, result.DomainNameRecords, 1)
	got, want := result.DomainNameRecords[0], sampleRecord()

	// Both lists follow the order of domainRecordProperties, so each case names its property.
	mapped := []any{got.Label, got.Required, got.RecordType, got.Name,
		got.RecordEntries, got.TTL, got.AlternativeRecordEntries}
	source := []any{want.Label, want.Required, want.RecordType, want.Name,
		want.RecordEntries, want.TTL, want.AlternativeRecordEntries}
	require.Len(t, mapped, len(domainRecordProperties))
	require.Len(t, source, len(domainRecordProperties))

	for i, property := range domainRecordProperties {
		t.Run(property, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, source[i], mapped[i])
		})
	}
}

func TestGetDomainMapsEveryDomainDtoProperty(t *testing.T) {
	t.Parallel()
	fn, mock := domainFunction(t)
	dto := sampleDomainDto()
	mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).Return(dto, nil).Times(1)

	got, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
	require.NoError(t, err)

	assert.Equal(t, testDomainID, got.DomainID)
	assert.Equal(t, dto.Domain, got.Domain)
	assert.Equal(t, DomainTypeSMTP, got.DomainType)
	assert.Equal(t, dto.VerificationToken, got.VerificationToken)
	assert.Equal(t, dto.DkimTokens, got.DkimTokens)
	assert.True(t, got.IsVerified)
	assert.True(t, got.HasMissingRecords)
	assert.Equal(t, dto.MissingRecordsMessage, got.MissingRecordsMessage)
	assert.True(t, got.HasDuplicateRecords)
	assert.Equal(t, dto.DuplicateRecordsMsg, got.DuplicateRecordsMessage)
	assert.Equal(t, dto.CatchAllInboxID, got.CatchAllInboxID)
	assert.Equal(t, dto.CreatedAt, got.CreatedAt)
	assert.Equal(t, dto.UpdatedAt, got.UpdatedAt)
}

func TestGetDomainAnswersAnEmptyRecordListWhenTheAPIOmitsOne(t *testing.T) {
	t.Parallel()
	fn, mock := domainFunction(t)
	mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).
		Return(&DomainDto{ID: testDomainID, DomainType: string(DomainTypeHTTP)}, nil).Times(1)

	got, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
	require.NoError(t, err)
	assert.NotNil(t, got.DomainNameRecords, "a stack loops over the records without a nil check")
	assert.Empty(t, got.DomainNameRecords)
	assert.NotNil(t, got.DkimTokens)
}

// api/openapi.json marks `alternativeRecordEntries` nullable, and the description tells you to loop
// over the records, so no record may reach the stack with a null list.
func TestGetDomainAnswersTheEmptyValuesTheAPIOmitsInsideEveryRecord(t *testing.T) {
	t.Parallel()
	fn, mock := domainFunction(t)
	mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).Return(&DomainDto{
		ID: testDomainID, DomainType: string(DomainTypeHTTP),
		MissingRecordsMessage: ptr(""), DuplicateRecordsMsg: ptr(""), CatchAllInboxID: ptr(""),
		DomainNameRecords: []DomainNameRecord{{Label: "VERIFICATION", RecordType: "TXT"}},
	}, nil).Times(1)

	got, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
	require.NoError(t, err)

	// The API answers the empty string for every message the domain does not carry.
	for name, value := range map[string]*string{
		"missingRecordsMessage":   got.MissingRecordsMessage,
		"duplicateRecordsMessage": got.DuplicateRecordsMessage,
		"catchAllInboxId":         got.CatchAllInboxID,
	} {
		assert.Nil(t, value, "the empty string reached the stack as the %q value", name)
	}

	require.Len(t, got.DomainNameRecords, 1)
	record := got.DomainNameRecords[0]
	assert.NotNil(t, record.RecordEntries, "a stack loops over the record entries without a nil check")
	assert.Empty(t, record.RecordEntries)
	assert.NotNil(t, record.AlternativeRecordEntries,
		"a stack loops over the alternative record entries without a nil check")
	assert.Empty(t, record.AlternativeRecordEntries)
}

func TestGetDomainSendsCheckForErrorsOnlyWhenYouSetIt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input *bool
		want  bool
	}{
		{"absent", nil, false},
		{"false", ptr(false), false},
		{"true", ptr(true), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fn, mock := domainFunction(t)
			mock.EXPECT().GetDomain(gomock.Any(), testDomainID, c.want).
				Return(sampleDomainDto(), nil).Times(1)

			_, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID, CheckForErrors: c.input})
			require.NoError(t, err)
		})
	}
}

// A function that answered an empty result for a missing inbox would hand the stack a silent
// blank. The error names the value to check instead.
func TestGetInboxReportsAnActionableErrorWhenTheInboxIsGone(t *testing.T) {
	t.Parallel()
	fn, mock := inboxFunction(t)
	mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	}).Times(1)

	got, err := readInbox(t, fn, testInboxID)
	require.Error(t, err, "a missing inbox must fail the function, not answer an empty result")
	assert.Equal(t,
		"MailSlurp has no inbox with the identifier `inbox-1`. Check the `inboxId` property and try again.",
		err.Error())
	assert.Zero(t, got)
}

func TestGetDomainReportsAnActionableErrorWhenTheDomainIsGone(t *testing.T) {
	t.Parallel()
	fn, mock := domainFunction(t)
	mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	}).Times(1)

	got, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
	require.Error(t, err, "a missing domain must fail the function, not answer an empty result")
	assert.Equal(t,
		"MailSlurp has no domain with the identifier `domain-1`. Check the `domainId` property and try again.",
		err.Error())
	assert.Zero(t, got)
}

// One situation takes one wording. A refusal that names the same empty property two ways reads as
// two different problems, and the reader looks for a second cause.
func TestEveryRefusalOfABlankValueNamesThePropertyTheSameWay(t *testing.T) {
	t.Parallel()
	const wanted = "property is empty"
	for name, message := range map[string]string{
		"the inbox function":       blankInboxIDMessage,
		"the domain function":      blankDomainIDMessage,
		"the required input check": emptyRequiredInputMessage,
	} {
		assert.Contains(t, message, wanted, "%s must name the empty property the same way", name)
	}
}

// A blank identifier once reached the transport guard, which names the client operation rather
// than the property. The function boundary names the property, and never calls the API.
func TestBothFunctionsRefuseABlankIdentifierBeforeTheyCallTheAPI(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, value string }{
		{"the empty string", ""},
		{"one space", " "},
		{"a tab and a newline", "\t\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// Neither mock carries an expectation, so a client call of any kind fails the case.
			inbox, _ := inboxFunction(t)
			_, err := readInbox(t, inbox, c.value)
			require.Error(t, err)
			assert.Equal(t,
				"The `inboxId` property is empty. Set the identifier of the inbox to read and try again.",
				err.Error())

			domain, _ := domainFunction(t)
			_, err = readDomain(t, domain, GetDomainArgs{DomainID: c.value})
			require.Error(t, err)
			assert.Equal(t,
				"The `domainId` property is empty. Set the identifier of the domain to read and try again.",
				err.Error())
		})
	}
}

// An unrouted path answers 404 too. Only the documented error code means the entity is gone, so
// every other failure reaches the user as the API reported it.
func TestBothFunctionsPassAnyOtherFailureThrough(t *testing.T) {
	t.Parallel()

	t.Run("getInbox", func(t *testing.T) {
		t.Parallel()
		fn, mock := inboxFunction(t)
		mock.EXPECT().GetInbox(gomock.Any(), testInboxID).
			Return(nil, &APIError{StatusCode: http.StatusNotFound}).Times(1)

		_, err := readInbox(t, fn, testInboxID)
		require.Error(t, err)
		var apiErr *APIError
		assert.ErrorAs(t, err, &apiErr, "the failure must reach the user as the API reported it")
	})

	t.Run("getDomain", func(t *testing.T) {
		t.Parallel()
		fn, mock := domainFunction(t)
		mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).
			Return(nil, &APIError{StatusCode: http.StatusForbidden}).Times(1)

		_, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
		require.Error(t, err)
		var apiErr *APIError
		assert.ErrorAs(t, err, &apiErr, "the failure must reach the user as the API reported it")
	})
}

// gomock fails the test on any call the case did not expect. Each case expects one read, so a
// mutating call inside either function fails here.
func TestNeitherFunctionReachesAMutatingClientMethod(t *testing.T) {
	t.Parallel()

	t.Run("getInbox", func(t *testing.T) {
		t.Parallel()
		fn, mock := inboxFunction(t)
		mock.EXPECT().GetInbox(gomock.Any(), testInboxID).Return(sampleInboxDto(), nil).Times(1)

		_, err := readInbox(t, fn, testInboxID)
		require.NoError(t, err)
	})

	t.Run("getDomain", func(t *testing.T) {
		t.Parallel()
		fn, mock := domainFunction(t)
		mock.EXPECT().GetDomain(gomock.Any(), testDomainID, false).
			Return(sampleDomainDto(), nil).Times(1)

		_, err := readDomain(t, fn, GetDomainArgs{DomainID: testDomainID})
		require.NoError(t, err)
	})
}

// A provider that never ran Configure carries no configuration, and a Configure that failed can
// leave a configuration without a client. Both functions must refuse both.
func TestBothFunctionsFailWhenTheProviderIsNotConfigured(t *testing.T) {
	t.Parallel()
	for title, config := range map[string]*Config{
		titleWithoutConfiguration: nil,
		titleWithoutClient:        {},
	} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			_, err := readInbox(t, &GetInbox{config: config}, testInboxID)
			requireNotConfigured(t, err)

			_, err = readDomain(t, &GetDomain{config: config}, GetDomainArgs{DomainID: testDomainID})
			requireNotConfigured(t, err)
		})
	}
}

// functionDescriptions returns every user-facing string the two functions ship, keyed by location.
func functionDescriptions(t *testing.T) map[string]string {
	t.Helper()
	doc := getFunctionSchema(t)
	out := map[string]string{}
	for token, fn := range doc.Functions {
		out[token] = fn.Description
		require.NotNil(t, fn.Inputs, "%s has no input type", token)
		require.NotNil(t, fn.Outputs, "%s has no output type", token)
		for name, prop := range fn.Inputs.Properties {
			out[token+".in."+name] = prop.Description
		}
		for name, prop := range fn.Outputs.Properties {
			out[token+".out."+name] = prop.Description
		}
	}
	record := doc.Types[domainRecordType]
	require.NotNil(t, record)
	for name, prop := range record.Properties {
		out[domainRecordType+"."+name] = prop.Description
	}
	return out
}

// The same writing rules the resource descriptions follow, applied to the function documentation.
func TestEveryFunctionDescriptionFollowsTheWritingRules(t *testing.T) {
	t.Parallel()
	all := functionDescriptions(t)
	require.NotEmpty(t, all)

	for where, text := range all {
		require.NotEmpty(t, text, "%s has no description", where)
		prose := backticked.ReplaceAllString(text, "X")
		assert.Empty(t, bannedWords.FindAllString(prose, -1), "%s uses a banned word", where)
		for _, phrase := range bannedPhrases {
			assert.NotContains(t, strings.ToLower(prose), phrase, "%s uses a banned phrase", where)
		}

		sentences := splitSentences(text)
		require.NotEmpty(t, sentences, "%s has no sentence", where)
		for _, sentence := range sentences {
			words := strings.Fields(backticked.ReplaceAllString(sentence, "X"))
			assert.LessOrEqual(t, len(words), descriptiveWordLimit,
				"%s: sentence of %d words: %s", where, len(words), sentence)
		}
		first := strings.Fields(sentences[0])[0]
		assert.Contains(t, allowedOpeners, strings.Trim(first, "*"),
			"%s drops the subject: %s", where, sentences[0])
	}
}

// The getDomain skip decision. The account decides whether a domain exists to read, so the decision
// and its test are hermetic and the integration legs read them from here.

// noDomainsReason names the precondition the getDomain legs need. A new domain needs DNS records
// in a zone this suite does not control, so the suite cannot create one to read.
const noDomainsReason = "this MailSlurp account holds no domain: add a verified domain to run " +
	"the getDomain integration legs, because a new domain needs DNS records outside this suite"

// domainSummary is the part of the unpaginated domain list these tests read.
type domainSummary struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}

// skipReasonForDomains answers the reason to skip a getDomain leg, or the empty string when the
// account holds a domain to read. One domain is enough to run every leg.
func skipReasonForDomains(found []domainSummary) string {
	if len(found) == 0 {
		return noDomainsReason
	}
	return ""
}

// A domain-bearing account must run the getDomain legs. Only an account with no domain skips them,
// and this test pins that decision, because no test can create a domain to prove it live.
func TestTheGetDomainLegsSkipOnlyWhenTheAccountHoldsNoDomain(t *testing.T) {
	t.Parallel()
	assert.Equal(t, noDomainsReason, skipReasonForDomains(nil))
	assert.Equal(t, noDomainsReason, skipReasonForDomains([]domainSummary{}))
	assert.Empty(t, skipReasonForDomains([]domainSummary{{ID: testDomainID, Domain: "mail.example.com"}}),
		"an account that holds one domain must run every getDomain leg")
}
