package internal

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const clientTestTemplateDetail = `{
  "id": "` + clientTestTemplateID + `",
  "name": "welcome",
  "content": "Hello {{firstName}}",
  "variables": [{"name":"firstName","variableType":"STRING"}],
  "createdAt": "2026-08-18T15:24:48.111Z"
}`

func TestClientCreateTemplatePostsNameAndContent(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestTemplateDetail))

	template, err := c.CreateTemplate(t.Context(), TemplateOptions{
		Name:    "welcome",
		Content: "Hello {{firstName}}",
	})
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "/templates", got.Path)

	body := got.body(t)
	require.Equal(t, "welcome", body["name"])
	require.Equal(t, "Hello {{firstName}}", body["content"])

	require.Equal(t, clientTestTemplateID, template.ID)
	require.Len(t, template.Variables, 1)
	require.Equal(t, "firstName", template.Variables[0].Name)
	require.Equal(t, "STRING", template.Variables[0].VariableType)
}

func TestClientGetTemplateReadsTheDetailEndpoint(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusOK, clientTestTemplateDetail))

	template, err := c.GetTemplate(t.Context(), clientTestTemplateID)
	require.NoError(t, err)

	require.Equal(t, "/templates/"+clientTestTemplateID, rec.only(t).Path)
	require.Equal(t, "welcome", template.Name)
}

func TestClientUpdateTemplatePutsBothRequiredProperties(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusCreated, clientTestTemplateDetail))

	_, err := c.UpdateTemplate(t.Context(), clientTestTemplateID, TemplateOptions{
		Name:    "welcome",
		Content: "Hello again",
	})
	require.NoError(t, err)

	got := rec.only(t)
	require.Equal(t, http.MethodPut, got.Method)
	require.Equal(t, "/templates/"+clientTestTemplateID, got.Path)

	body := got.body(t)
	require.Contains(t, body, "name", "both properties are required on every write")
	require.Equal(t, "Hello again", body["content"])
}

func TestClientDeleteTemplate(t *testing.T) {
	c, rec := clientTestServer(t, clientTestReply(http.StatusNoContent, ""))

	require.NoError(t, c.DeleteTemplate(t.Context(), clientTestTemplateID))

	got := rec.only(t)
	require.Equal(t, http.MethodDelete, got.Method)
	require.Equal(t, "/templates/"+clientTestTemplateID, got.Path)
}
