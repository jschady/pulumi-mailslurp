package internal

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestWebhookDetail = `{
  "id": "` + clientTestWebhookID + `",
  "basicAuth": false,
  "name": "bs2-webhook",
  "phoneId": null,
  "inboxId": null,
  "requestBodyTemplate": "{\"probe\":\"8ce3bbb3\"}",
  "url": "https://example.com/bs2",
  "method": "POST",
  "payloadJsonSchema": "",
  "createdAt": "2026-08-18T15:27:08.493Z",
  "updatedAt": "2026-08-18T15:27:08.493Z",
  "eventName": "NEW_CONTACT",
  "requestHeaders": {"headers":[{"name":"x-bs2-probe","value":"bs2-secret-value"}]},
  "ignoreInsecureSslCertificates": null,
  "useStaticIpRange": null,
  "healthStatus": null,
  "enabled": null
}`

func clientTestWebhookOptions() WebhookOptions {
	return WebhookOptions{
		URL:                 "https://example.com/bs2",
		Name:                "bs2-webhook",
		EventName:           "NEW_CONTACT",
		RequestBodyTemplate: `{"probe":"8ce3bbb3"}`,
		IncludeHeaders: &WebhookHeaders{
			Headers: []WebhookHeader{{Name: "x-bs2-probe", Value: "bs2-secret-value"}},
		},
		Tags: []string{"probe"},
	}
}

func TestClientCreateInboxWebhookPostsUnderTheInbox(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestWebhookDetail))

	hook, err := c.CreateInboxWebhook(t.Context(), clientTestInboxID, clientTestWebhookOptions())
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "/inboxes/"+clientTestInboxID+"/webhooks", got.Path)
	require.Equal(t, clientTestWebhookID, hook.ID)
}

func TestClientCreateAccountWebhookPostsWithoutAnInbox(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestWebhookDetail))

	_, err := c.CreateAccountWebhook(t.Context(), clientTestWebhookOptions())
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, "/webhooks", got.Path, "an account webhook costs no inbox quota")
	require.Empty(t, got.Query.Get("inboxId"))
}

func TestClientWebhookWritesIncludeHeadersAndReadsRequestHeaders(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestWebhookDetail))

	hook, err := c.CreateAccountWebhook(t.Context(), clientTestWebhookOptions())
	require.NoError(t, err)

	body := rec.only(t).body(t)
	require.Contains(t, body, "includeHeaders", "the write property is includeHeaders")
	require.NotContains(t, body, "requestHeaders")

	require.NotNil(t, hook.RequestHeaders, "the read property is requestHeaders")
	require.Len(t, hook.RequestHeaders.Headers, 1)
	require.Equal(t, "x-bs2-probe", hook.RequestHeaders.Headers[0].Name)
	require.Equal(t, "bs2-secret-value", hook.RequestHeaders.Headers[0].Value)
}

func TestClientCreateWebhookOmitsIncludeHeadersWhenThereAreNone(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestWebhookDetail))

	opts := clientTestWebhookOptions()
	opts.IncludeHeaders = nil
	_, err := c.CreateAccountWebhook(t.Context(), opts)
	require.NoError(t, err)

	require.NotContains(t, string(rec.only(t).Body), `"includeHeaders"`,
		"the API requires the headers list, so a null header object must never travel")
}

func TestClientUpdateWebhookSendsTheWholeOptionsBody(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestWebhookDetail))

	_, err := c.UpdateWebhook(t.Context(), clientTestWebhookID, nil, clientTestWebhookOptions())
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPatch, got.Method)
	require.Equal(t, "/webhooks/"+clientTestWebhookID, got.Path)

	// The PATCH replaces the options wholesale: an omitted property is destroyed.
	body := got.body(t)
	for _, key := range []string{"url", "name", "eventName", "requestBodyTemplate", "includeHeaders"} {
		require.Contains(t, body, key, "a wholesale replace must carry %s", key)
	}
}

func TestClientUpdateWebhookAlwaysSendsTheEventName(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestWebhookDetail))

	_, err := c.UpdateWebhook(t.Context(), clientTestWebhookID, nil, WebhookOptions{
		URL: "https://example.com/only-the-url",
	})
	require.NoError(t, err)

	body := rec.only(t).body(t)
	require.Contains(t, body, "eventName",
		"an omitted eventName reverts to EMAIL_RECEIVED and changes what the webhook fires on")
	require.Contains(t, body, "url")
}

func TestClientUpdateWebhookMovesTheInboxWithAQueryParameter(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestWebhookDetail))

	_, err := c.UpdateWebhook(t.Context(), clientTestWebhookID,
		clientTestPtr(clientTestInboxID), clientTestWebhookOptions())
	require.NoError(t, err)
	require.Equal(t, clientTestInboxID, rec.at(t, 0).Query.Get("inboxId"))

	_, err = c.UpdateWebhook(t.Context(), clientTestWebhookID, nil, clientTestWebhookOptions())
	require.NoError(t, err)
	require.NotContains(t, rec.at(t, 1).Query, "inboxId")
}

func TestClientWebhookOptionsCannotExpressAPartialUpdate(t *testing.T) {
	iface := reflect.TypeOf((*Client)(nil)).Elem()
	update, found := iface.MethodByName("UpdateWebhook")
	require.True(t, found)

	options := reflect.TypeOf(WebhookOptions{})
	require.Equal(t, options, update.Type.In(update.Type.NumIn()-1),
		"the update must take the same whole options body the create takes")

	create, found := iface.MethodByName("CreateAccountWebhook")
	require.True(t, found)
	require.Equal(t, options, create.Type.In(create.Type.NumIn()-1))

	for i := range options.NumField() {
		field := options.Field(i)
		pointerToString := field.Type.Kind() == reflect.Pointer &&
			field.Type.Elem().Kind() == reflect.String
		require.False(t, pointerToString,
			"%s must not be a pointer to a string: no property may mean leave alone", field.Name)

		if field.Name == "EventName" {
			require.Equal(t, "eventName", field.Tag.Get("json"),
				"eventName must carry no omitempty; omitting it resets the subscription")
		}
	}
}

func TestClientUpdateWebhookHeadersUsesTheHeadersEndpoint(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestWebhookDetail))

	headers := WebhookHeaders{Headers: []WebhookHeader{{Name: "x-key", Value: "restored"}}}
	hook, err := c.UpdateWebhookHeaders(t.Context(), clientTestWebhookID, headers)
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPut, got.Method)
	require.Equal(t, "/webhooks/"+clientTestWebhookID+"/headers", got.Path,
		"the headers endpoint writes headers without destroying anything else")
	require.Contains(t, string(got.Body), `"x-key"`)
	require.NotNil(t, hook.RequestHeaders)
}

func TestClientGetWebhookReadsTheDetailEndpoint(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestWebhookDetail))

	hook, err := c.GetWebhook(t.Context(), clientTestWebhookID)
	require.NoError(t, err)

	require.Equal(t, "/webhooks/"+clientTestWebhookID, rec.only(t).Path,
		"the list projection omits requestHeaders and requestBodyTemplate")
	require.Equal(t, "NEW_CONTACT", hook.EventName)
	require.Equal(t, `{"probe":"8ce3bbb3"}`, *hook.RequestBodyTemplate)
	require.Nil(t, hook.HealthStatus)
	require.Nil(t, hook.Enabled)
}

func TestClientDeleteWebhookAcceptsATwoHundred(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, ""))

	require.NoError(t, c.DeleteWebhook(t.Context(), clientTestWebhookID))

	got := rec.only(t)
	require.Equal(t, http.MethodDelete, got.Method)
	require.Equal(t, "/webhooks/"+clientTestWebhookID, got.Path,
		"deleteAllWebhooks lives at /webhooks and must never be reachable")
}
