package internal

import (
	"context"
	"net/http"
)

const templatesPath = "/templates"

// TemplateVariable is a variable the server parsed out of the template content.
type TemplateVariable struct {
	Name         string `json:"name"`
	VariableType string `json:"variableType"`
}

// TemplateDto is the email template the API returns.
type TemplateDto struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Content   string             `json:"content"`
	Variables []TemplateVariable `json:"variables"`
	CreatedAt string             `json:"createdAt"`
}

// TemplateOptions is the whole template body. The API requires both properties on every write.
type TemplateOptions struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (c *client) CreateTemplate(ctx context.Context, opts TemplateOptions) (*TemplateDto, error) {
	return call[TemplateDto](ctx, c, apiRequest{
		method:    http.MethodPost,
		path:      templatesPath,
		operation: "createTemplate",
		body:      opts,
	})
}

func (c *client) GetTemplate(ctx context.Context, templateID string) (*TemplateDto, error) {
	return call[TemplateDto](ctx, c, apiRequest{
		method:    http.MethodGet,
		path:      pathOf(templatesPath, templateID),
		operation: "getTemplate",
	})
}

func (c *client) UpdateTemplate(
	ctx context.Context, templateID string, opts TemplateOptions,
) (*TemplateDto, error) {
	return call[TemplateDto](ctx, c, apiRequest{
		method:    http.MethodPut,
		path:      pathOf(templatesPath, templateID),
		operation: "updateTemplate",
		body:      opts,
	})
}

func (c *client) DeleteTemplate(ctx context.Context, templateID string) error {
	return c.do(ctx, apiRequest{
		method:    http.MethodDelete,
		path:      pathOf(templatesPath, templateID),
		operation: "deleteTemplate",
	}, nil)
}
