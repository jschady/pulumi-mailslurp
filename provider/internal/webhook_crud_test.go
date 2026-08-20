package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// webhookResource builds the resource with a mock client already in place, which is what
// Configure does in production.
func webhookResource(t *testing.T) (*Webhook, *MockClient) {
	t.Helper()
	mock := NewMockClient(gomock.NewController(t))
	return &Webhook{config: &Config{client: mock}}, mock
}

func TestWebhookCreateWithoutAnInboxUsesTheAccountEndpoint(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	var got WebhookOptions
	mock.EXPECT().CreateAccountWebhook(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts WebhookOptions) (*WebhookDto, error) {
			got = opts
			return &WebhookDto{ID: testWebhookID, URL: opts.URL, EventName: opts.EventName}, nil
		})

	resp, err := webhook.Create(context.Background(), infer.CreateRequest[WebhookArgs]{
		Inputs: WebhookArgs{
			URL:                           testWebhookURL,
			Name:                          ptr(testWebhookName),
			EventName:                     ptr(WebhookEventNewContact),
			IncludeHeaders:                map[string]string{testHeaderName: testHeaderValue},
			RequestBodyTemplate:           ptr(`{"a":1}`),
			Tags:                          []string{"a", "b"},
			UseStaticIPRange:              ptr(true),
			IgnoreInsecureSSLCertificates: ptr(true),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, testWebhookID, resp.ID)

	assert.Equal(t, testWebhookURL, got.URL)
	assert.Equal(t, testWebhookName, got.Name)
	assert.Equal(t, string(WebhookEventNewContact), got.EventName)
	assert.Equal(t, `{"a":1}`, got.RequestBodyTemplate)
	assert.Equal(t, []string{"a", "b"}, got.Tags)
	require.NotNil(t, got.IncludeHeaders)
	require.Len(t, got.IncludeHeaders.Headers, 1)
	assert.Equal(t, testHeaderName, got.IncludeHeaders.Headers[0].Name)
	assert.Equal(t, testHeaderValue, got.IncludeHeaders.Headers[0].Value)
	require.NotNil(t, got.UseStaticIPRange)
	assert.True(t, *got.UseStaticIPRange)
	require.NotNil(t, got.IgnoreInsecureSSLCertificates)
	assert.True(t, *got.IgnoreInsecureSSLCertificates)
}

func TestWebhookCreateWithAnInboxUsesTheInboxEndpoint(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().CreateInboxWebhook(gomock.Any(), testInboxID, gomock.Any()).
		Return(&WebhookDto{ID: testWebhookID, URL: testWebhookURL, InboxID: ptr(testInboxID)}, nil)

	resp, err := webhook.Create(context.Background(), infer.CreateRequest[WebhookArgs]{
		Inputs: WebhookArgs{URL: testWebhookURL, InboxID: ptr(testInboxID)},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Output.InboxID)
	assert.Equal(t, testInboxID, *resp.Output.InboxID)
}

// The create response carries the headers in full, under the read property name.
func TestWebhookCreateReadsTheHeadersFromTheResponse(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().CreateAccountWebhook(gomock.Any(), gomock.Any()).Return(&WebhookDto{
		ID:  testWebhookID,
		URL: testWebhookURL,
		RequestHeaders: &WebhookHeaders{
			Headers: []WebhookHeader{{Name: testHeaderName, Value: testHeaderValueNext}},
		},
	}, nil)

	resp, err := webhook.Create(context.Background(), infer.CreateRequest[WebhookArgs]{
		Inputs: WebhookArgs{URL: testWebhookURL, IncludeHeaders: map[string]string{testHeaderName: testHeaderValue}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{testHeaderName: testHeaderValueNext}, resp.Output.IncludeHeaders,
		"the response value, not the input value, is what the create answers")
}

// The API answers no eventName for some webhooks, and an empty subscription is not a state the
// server holds: it fires on the default event.
func TestWebhookCreateStoresTheDefaultEventName(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	var got WebhookOptions
	mock.EXPECT().CreateAccountWebhook(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts WebhookOptions) (*WebhookDto, error) {
			got = opts
			return &WebhookDto{ID: testWebhookID, URL: opts.URL, EventName: ""}, nil
		})

	resp, err := webhook.Create(context.Background(), infer.CreateRequest[WebhookArgs]{
		Inputs: WebhookArgs{URL: testWebhookURL},
	})
	require.NoError(t, err)
	assert.Equal(t, string(defaultWebhookEventName), got.EventName, "the body always names the event")
	assert.Equal(t, defaultWebhookEventName, resp.Output.EventName)
}

func TestWebhookCreateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	webhook, _ := webhookResource(t)
	resp, err := webhook.Create(context.Background(), infer.CreateRequest[WebhookArgs]{
		Inputs: WebhookArgs{URL: testWebhookURL, Name: ptr("preview")},
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.ID, "a preview must not claim an id")
	assert.Equal(t, testWebhookURL, resp.Output.URL)
}

// A provider that never ran Configure carries no configuration, and a Configure that failed can
// leave a configuration without a client. The create must refuse both.
func TestWebhookCreateFailsWhenTheProviderIsNotConfigured(t *testing.T) {
	t.Parallel()
	for title, webhook := range map[string]*Webhook{
		titleWithoutConfiguration: {},
		titleWithoutClient:        {config: &Config{}},
	} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			_, err := webhook.Create(context.Background(), infer.CreateRequest[WebhookArgs]{})
			requireNotConfigured(t, err)
		})
	}
}

// The list projection omits requestHeaders, requestBodyTemplate and method, so a
// refresh from it would blank them. The client carries no webhook list method at all.
func TestWebhookReadUsesTheDetailEndpointAndNeverAListProjection(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	// The controller fails the test on any call this expectation does not name.
	mock.EXPECT().GetWebhook(gomock.Any(), testWebhookID).
		Return(&WebhookDto{ID: testWebhookID, URL: testWebhookURL, Method: "POST"}, nil)

	resp, err := webhook.Read(context.Background(), infer.ReadRequest[WebhookArgs, WebhookState]{
		ID:    testWebhookID,
		State: WebhookState{URL: testWebhookURL},
	})
	require.NoError(t, err)
	assert.Equal(t, testWebhookID, resp.ID)
	assert.Equal(t, "POST", resp.State.Method)
}

func TestWebhookReadMapsRequestHeadersOntoIncludeHeaders(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().GetWebhook(gomock.Any(), testWebhookID).Return(&WebhookDto{
		ID:        testWebhookID,
		URL:       testWebhookURL,
		Name:      ptr("renamed on the server"),
		EventName: string(WebhookEventNewContact),
		RequestHeaders: &WebhookHeaders{Headers: []WebhookHeader{
			{Name: testHeaderName, Value: testHeaderValue},
			{Name: testHeaderNameNext, Value: "another value"},
		}},
		RequestBodyTemplate: ptr(`{"a":1}`),
		HealthStatus:        ptr("HEALTHY"),
		Enabled:             ptr(true),
	}, nil)

	resp, err := webhook.Read(context.Background(), infer.ReadRequest[WebhookArgs, WebhookState]{
		ID:    testWebhookID,
		State: WebhookState{URL: testWebhookURL},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{testHeaderName: testHeaderValue, testHeaderNameNext: "another value"},
		resp.State.IncludeHeaders)
	assert.Equal(t, "renamed on the server", *resp.State.Name)
	assert.Equal(t, WebhookEventNewContact, resp.State.EventName)
	assert.Equal(t, "HEALTHY", *resp.State.HealthStatus)
	assert.True(t, *resp.State.Enabled)
}

// The API answers null for the headers of a webhook that carries none, and null is not an empty
// map: reading it as one would plan an update forever.
func TestWebhookReadTreatsNullHeadersAsUnset(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().GetWebhook(gomock.Any(), testWebhookID).
		Return(&WebhookDto{ID: testWebhookID, URL: testWebhookURL, RequestHeaders: nil}, nil)

	resp, err := webhook.Read(context.Background(), infer.ReadRequest[WebhookArgs, WebhookState]{
		ID:    testWebhookID,
		State: WebhookState{URL: testWebhookURL},
	})
	require.NoError(t, err)
	assert.Nil(t, resp.State.IncludeHeaders)
	assert.False(t, resp.State.UseStaticIPRange, "a null flag reads as false")
	assert.False(t, resp.State.IgnoreInsecureSSLCertificates)
}

func TestWebhookReadOnAMissingWebhookReturnsAnEmptyResponse(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().GetWebhook(gomock.Any(), testWebhookID).Return(nil, &APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	resp, err := webhook.Read(context.Background(), infer.ReadRequest[WebhookArgs, WebhookState]{
		ID:    testWebhookID,
		State: WebhookState{URL: testWebhookURL},
	})
	require.NoError(t, err, "a deleted webhook is drift, not a failure")
	assert.Empty(t, resp.ID, "an empty id tells the engine the webhook is gone")
}

// An unrouted path answers 404 too. Only the documented error code means the webhook is gone.
func TestWebhookReadReturnsTheErrorForAnyOtherFailure(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().GetWebhook(gomock.Any(), testWebhookID).
		Return(nil, &APIError{StatusCode: http.StatusNotFound})

	_, err := webhook.Read(context.Background(), infer.ReadRequest[WebhookArgs, WebhookState]{
		ID: testWebhookID,
	})
	require.Error(t, err)
}

// sampleWebhookDto is a webhook with every reported property set, which is what an import reads.
func sampleWebhookDto() *WebhookDto {
	return &WebhookDto{
		ID:        testWebhookID,
		URL:       testWebhookURL,
		Name:      ptr(testWebhookName),
		EventName: string(WebhookEventNewContact),
		InboxID:   ptr(testInboxID),
		RequestHeaders: &WebhookHeaders{Headers: []WebhookHeader{
			{Name: testHeaderName, Value: testHeaderValue},
		}},
		RequestBodyTemplate:           ptr(`{"a":1}`),
		Method:                        http.MethodPost,
		CreatedAt:                     ptr("2026-01-02T03:04:05.000Z"),
		HealthStatus:                  ptr("HEALTHY"),
		Enabled:                       ptr(true),
		UseStaticIPRange:              ptr(true),
		IgnoreInsecureSSLCertificates: ptr(true),
	}
}

// An import calls Read with no input and no prior state, so the API answer is the only source of
// the inputs. Without them `pulumi import` writes a program with no property, and preview fails.
func TestWebhookReadAnswersTheInputsThatAnImportDoesNotCarry(t *testing.T) {
	t.Parallel()
	mock := NewMockClient(gomock.NewController(t))
	mock.EXPECT().GetWebhook(gomock.Any(), testWebhookID).Return(sampleWebhookDto(), nil)

	resp, err := serverWithClient(t, mock).Read(p.ReadRequest{
		Urn:        webhookURN(),
		ID:         testWebhookID,
		Properties: property.NewMap(nil),
		Inputs:     property.NewMap(nil),
	})
	require.NoError(t, err)
	require.Equal(t, testWebhookID, resp.ID)

	want := map[string]property.Value{
		webhookPropURL:                           property.New(testWebhookURL),
		webhookPropName:                          property.New(testWebhookName),
		webhookPropEventName:                     property.New(string(WebhookEventNewContact)),
		webhookPropRequestBodyTemplate:           property.New(`{"a":1}`),
		webhookPropInboxID:                       property.New(testInboxID),
		webhookPropUseStaticIPRange:              property.New(true),
		webhookPropIgnoreInsecureSSLCertificates: property.New(true),
		webhookPropIncludeHeaders: property.New(property.NewMap(map[string]property.Value{
			testHeaderName: property.New(testHeaderValue),
		})),
	}
	for name, value := range want {
		got, ok := resp.Inputs.GetOk(name)
		require.True(t, ok, "an import writes no %s into the program", name)
		assert.Equal(t, value, got.WithSecret(false), "the imported %s", name)
	}

	// MailSlurp accepts the tags and never reports them, so an import cannot answer them and the
	// generated program states nothing about them.
	_, ok := resp.Inputs.GetOk(webhookPropTags)
	assert.False(t, ok, "an import invents the tags, which no answer carries")
}

// The update replaces the options, so every property the body omits is destroyed and
// an omitted eventName silently changes what the webhook fires on.
func TestWebhookUpdateSendsTheWholeOptionsBody(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	var got WebhookOptions
	mock.EXPECT().UpdateWebhook(gomock.Any(), testWebhookID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ *string, opts WebhookOptions) (*WebhookDto, error) {
			got = opts
			return &WebhookDto{ID: testWebhookID, URL: opts.URL, EventName: opts.EventName}, nil
		})

	_, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
		ID:    testWebhookID,
		State: WebhookState{URL: testWebhookURL, EventName: WebhookEventNewContact},
		Inputs: WebhookArgs{
			URL:                 testWebhookURLNext,
			Name:                ptr("a name"),
			EventName:           ptr(WebhookEventNewContact),
			IncludeHeaders:      map[string]string{testHeaderName: testHeaderValue},
			RequestBodyTemplate: ptr(`{"a":1}`),
			Tags:                []string{"a"},
		},
	})
	require.NoError(t, err)

	body, err := json.Marshal(got)
	require.NoError(t, err)
	for _, key := range []string{
		webhookPropURL, webhookPropName, webhookPropEventName, webhookPropRequestBodyTemplate,
		webhookPropIncludeHeaders, webhookPropTags, webhookPropUseStaticIPRange,
		webhookPropIgnoreInsecureSSLCertificates,
	} {
		assert.Contains(t, string(body), `"`+key+`"`, "the whole body must carry %s", key)
	}
	assert.Equal(t, string(WebhookEventNewContact), got.EventName)
}

func TestWebhookUpdateMovesTheWebhookWithTheQueryParameter(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	var got *string
	mock.EXPECT().UpdateWebhook(gomock.Any(), testWebhookID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, inboxID *string, opts WebhookOptions) (*WebhookDto, error) {
			got = inboxID
			return &WebhookDto{ID: testWebhookID, URL: opts.URL, InboxID: inboxID}, nil
		})

	resp, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
		ID:     testWebhookID,
		State:  WebhookState{URL: testWebhookURL},
		Inputs: WebhookArgs{URL: testWebhookURL, InboxID: ptr(testInboxID)},
	})
	require.NoError(t, err)
	require.NotNil(t, got, "the move needs the query parameter")
	assert.Equal(t, testInboxID, *got)
	require.NotNil(t, resp.Output.InboxID)
	assert.Equal(t, testInboxID, *resp.Output.InboxID)
}

// The inbox rides the query parameter, and a call that omits it moves the webhook back to the
// account (probed live). So every update names the wanted inbox, and never a blank one.
func TestWebhookUpdateAlwaysNamesTheWantedInbox(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		state WebhookState
		in    WebhookArgs
		want  *string
	}{
		{"the webhook keeps its inbox",
			WebhookState{URL: testWebhookURL, InboxID: ptr(testInboxID)},
			WebhookArgs{URL: testWebhookURL, InboxID: ptr(testInboxID)},
			ptr(testInboxID)},
		{"the webhook moves onto an inbox",
			WebhookState{URL: testWebhookURL},
			WebhookArgs{URL: testWebhookURL, InboxID: ptr(testInboxID)},
			ptr(testInboxID)},
		{"the webhook moves to another inbox",
			WebhookState{URL: testWebhookURL, InboxID: ptr("inbox-old")},
			WebhookArgs{URL: testWebhookURL, InboxID: ptr(testInboxID)},
			ptr(testInboxID)},
		{"the webhook has no inbox",
			WebhookState{URL: testWebhookURL},
			WebhookArgs{URL: testWebhookURL},
			nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			webhook, mock := webhookResource(t)
			var got *string
			var called bool
			mock.EXPECT().UpdateWebhook(gomock.Any(), testWebhookID, gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ string, inboxID *string, _ WebhookOptions) (*WebhookDto, error) {
					got, called = inboxID, true
					return &WebhookDto{ID: testWebhookID, URL: testWebhookURL, InboxID: inboxID}, nil
				})

			_, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
				ID: testWebhookID, State: tc.state, Inputs: tc.in,
			})
			require.NoError(t, err)
			require.True(t, called)
			if tc.want == nil {
				assert.Nil(t, got, "a webhook without an inbox names none, and never a blank one")
				return
			}
			require.NotNil(t, got, "an omitted inbox moves the webhook back to the account")
			assert.Equal(t, *tc.want, *got)
			assert.NotEmpty(t, *got)
		})
	}
}

func TestWebhookUpdateWritesAHeaderChangeThroughTheHeadersEndpoint(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	var got WebhookHeaders
	// The controller fails the test if the full update runs instead.
	mock.EXPECT().UpdateWebhookHeaders(gomock.Any(), testWebhookID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, headers WebhookHeaders) (*WebhookDto, error) {
			got = headers
			return &WebhookDto{ID: testWebhookID, URL: testWebhookURL, RequestHeaders: &headers}, nil
		})

	resp, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
		ID: testWebhookID,
		State: WebhookState{
			URL:            testWebhookURL,
			EventName:      defaultWebhookEventName,
			IncludeHeaders: map[string]string{testHeaderName: testHeaderValue},
		},
		Inputs: WebhookArgs{
			URL:            testWebhookURL,
			IncludeHeaders: map[string]string{testHeaderName: testHeaderValueNext},
		},
	})
	require.NoError(t, err)
	require.Len(t, got.Headers, 1)
	assert.Equal(t, testHeaderName, got.Headers[0].Name)
	assert.Equal(t, testHeaderValueNext, got.Headers[0].Value)
	assert.Equal(t, map[string]string{testHeaderName: testHeaderValueNext}, resp.Output.IncludeHeaders)
}

// The headers endpoint takes a list, and an empty list clears the headers (probed live).
func TestWebhookUpdateClearsTheHeadersThroughTheHeadersEndpoint(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	var got WebhookHeaders
	mock.EXPECT().UpdateWebhookHeaders(gomock.Any(), testWebhookID, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, headers WebhookHeaders) (*WebhookDto, error) {
			got = headers
			return &WebhookDto{ID: testWebhookID, URL: testWebhookURL}, nil
		})

	_, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
		ID: testWebhookID,
		State: WebhookState{
			URL:            testWebhookURL,
			EventName:      defaultWebhookEventName,
			IncludeHeaders: map[string]string{testHeaderName: testHeaderValue},
		},
		Inputs: WebhookArgs{URL: testWebhookURL},
	})
	require.NoError(t, err)
	assert.Empty(t, got.Headers)
	assert.NotNil(t, got.Headers, "the API requires the list, so it must never travel as null")
}

// Any change beyond the headers needs the whole options body, so the headers endpoint is wrong.
func TestWebhookUpdateSendsTheWholeBodyWhenMoreThanTheHeadersChange(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().UpdateWebhook(gomock.Any(), testWebhookID, gomock.Any(), gomock.Any()).
		Return(&WebhookDto{ID: testWebhookID, URL: testWebhookURLNext}, nil)

	_, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
		ID: testWebhookID,
		State: WebhookState{
			URL:            testWebhookURL,
			EventName:      defaultWebhookEventName,
			IncludeHeaders: map[string]string{testHeaderName: testHeaderValue},
		},
		Inputs: WebhookArgs{
			URL:            testWebhookURLNext,
			IncludeHeaders: map[string]string{testHeaderName: testHeaderValueNext},
		},
	})
	require.NoError(t, err)
}

func TestWebhookUpdateCallsNoAPIMethodDuringAPreview(t *testing.T) {
	t.Parallel()
	webhook, _ := webhookResource(t)
	resp, err := webhook.Update(context.Background(), infer.UpdateRequest[WebhookArgs, WebhookState]{
		ID:     testWebhookID,
		State:  WebhookState{URL: testWebhookURL, Method: "POST"},
		Inputs: WebhookArgs{URL: testWebhookURLNext},
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Equal(t, testWebhookURLNext, resp.Output.URL)
	assert.Equal(t, "POST", resp.Output.Method, "the preview keeps what only the API can report")
}

func TestWebhookDeleteRemovesTheWebhook(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().DeleteWebhook(gomock.Any(), testWebhookID).Return(nil)

	_, err := webhook.Delete(context.Background(), infer.DeleteRequest[WebhookState]{ID: testWebhookID})
	require.NoError(t, err)
}

func TestWebhookDeleteToleratesAWebhookThatIsAlreadyGone(t *testing.T) {
	t.Parallel()
	webhook, mock := webhookResource(t)
	mock.EXPECT().DeleteWebhook(gomock.Any(), testWebhookID).Return(&APIError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  errorCodeEntityNotFound,
	})

	_, err := webhook.Delete(context.Background(), infer.DeleteRequest[WebhookState]{ID: testWebhookID})
	require.NoError(t, err)
}

// Webhook naming and sweep policy. The integration sweeper reads these, and this
// file carries no build tag, so both test tiers see them.
const testWebhookKind = "webhook"

// testWebhookNamePattern matches only what newTestName builds, so a webhook that fails it is
// never a sweep candidate.
var testWebhookNamePattern = regexp.MustCompile(`^` + testNamePrefix + testWebhookKind + `-[0-9a-f]{8}$`)

// isSweepableTestWebhook reports whether the sweeper may delete a webhook. A blank identifier
// would reach the whole collection, and a young webhook can belong to a concurrent run.
func isSweepableTestWebhook(id string, name *string, createdAt, now time.Time) bool {
	if id == "" || name == nil || !testWebhookNamePattern.MatchString(*name) {
		return false
	}
	return now.Sub(createdAt) > sweepMinAge
}

// The integration sweeper deletes webhooks from a live account, so its predicate answers false
// for every name this package did not build, and for an identifier it did not capture.
func TestTheSweeperMatchesOnlyTheWebhookNamesTheTestsBuild(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	now := old.Add(24 * time.Hour)

	for _, name := range []string{
		testWebhookName,
		"pulumi-test-webhook-00000000",
		"pulumi-test-webhook-ffffffff",
	} {
		assert.True(t, isSweepableTestWebhook(testWebhookID, &name, old, now), "must sweep %q", name)
	}

	for _, name := range []string{
		"", "a real webhook", testNamePrefix + testInboxKind + "-abcd1234", "pulumi-test-webhook-ABCD1234",
		"pulumi-test-webhook-abcd123", "pulumi-test-webhook-abcd12345", "pulumi-test-webhook-",
		"x " + testWebhookName, testWebhookName + " x", testWebhookName + "\n",
	} {
		assert.False(t, isSweepableTestWebhook(testWebhookID, &name, old, now), "must not sweep %q", name)
	}

	sweepable := testWebhookName
	assert.False(t, isSweepableTestWebhook("", &sweepable, old, now),
		"a blank identifier reaches the whole collection, so it is never a sweep candidate")
	assert.False(t, isSweepableTestWebhook(testWebhookID, nil, old, now),
		"a null name is never a sweep candidate")
	assert.False(t, isSweepableTestWebhook(testWebhookID, &sweepable, now.Add(-time.Minute), now),
		"a fresh webhook can belong to a concurrent run")
}
