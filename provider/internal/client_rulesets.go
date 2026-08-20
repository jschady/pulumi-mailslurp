package internal

import (
	"context"
	"net/http"
	"net/url"
)

const rulesetsPath = "/rulesets"

// RulesetDto is the inbox ruleset the API returns.
type RulesetDto struct {
	ID        string  `json:"id"`
	InboxID   *string `json:"inboxId"`
	PhoneID   *string `json:"phoneId"`
	Scope     string  `json:"scope"`
	Action    string  `json:"action"`
	Target    string  `json:"target"`
	Handler   string  `json:"handler"`
	CreatedAt string  `json:"createdAt"`
}

// RulesetOptions is the create body. The API requires all three properties and offers no update.
type RulesetOptions struct {
	Scope  string `json:"scope"`
	Action string `json:"action"`
	Target string `json:"target"`
}

// CreateRuleset creates the ruleset for one inbox. There is no update: PATCH /rulesets is a dry
// run named testNewRuleset, and PUT /rulesets is another dry run, so neither is wired here.
func (c *client) CreateRuleset(
	ctx context.Context, inboxID string, opts RulesetOptions,
) (*RulesetDto, error) {
	query := url.Values{}
	query.Set("inboxId", inboxID)

	return call[RulesetDto](ctx, c, apiRequest{
		method:    http.MethodPost,
		path:      rulesetsPath,
		operation: "createNewRuleset",
		query:     query,
		body:      opts,
	})
}

func (c *client) GetRuleset(ctx context.Context, rulesetID string) (*RulesetDto, error) {
	return call[RulesetDto](ctx, c, apiRequest{
		method:    http.MethodGet,
		path:      pathOf(rulesetsPath, rulesetID),
		operation: "getRuleset",
	})
}

func (c *client) DeleteRuleset(ctx context.Context, rulesetID string) error {
	return c.do(ctx, apiRequest{
		method:    http.MethodDelete,
		path:      pathOf(rulesetsPath, rulesetID),
		operation: "deleteRuleset",
	}, nil)
}
