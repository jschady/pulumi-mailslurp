package internal

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The list of readers the live probe walks, and the pin that keeps it whole. This file carries no
// build tag, so the pin runs without a credential.

// listReaderProbe is one kind's list reader, named for the message a failure carries.
type listReaderProbe struct {
	kind string
	read func(ctx context.Context, key string) error
}

// listReaderProbes is every reader the sweeps and the account checks depend on. A reader missing
// here can break without any test noticing.
func listReaderProbes() []listReaderProbe {
	return []listReaderProbe{
		{testInboxKind, func(ctx context.Context, key string) error {
			_, err := listRecentInboxes(ctx, key, 0)
			return err
		}},
		{testWebhookKind, func(ctx context.Context, key string) error {
			_, err := listWebhooks(ctx, key, 0)
			return err
		}},
		{testRulesetKind, func(ctx context.Context, key string) error {
			_, err := listRulesets(ctx, key, 0)
			return err
		}},
		{testTemplateKind, func(ctx context.Context, key string) error {
			_, err := listTemplates(ctx, key, 0)
			return err
		}},
		{testForwarderKind, func(ctx context.Context, key string) error {
			_, err := listForwarders(ctx, key, 0)
			return err
		}},
	}
}

// Every kind the sweeps cover depends on that kind's list reader, so each one needs a probe. A
// reader dropped from the list above would break without any test noticing.
func TestEveryKindTheSweepsCoverHasAListReaderProbe(t *testing.T) {
	t.Parallel()
	var swept, probed []string
	for _, sweep := range staleSweeps() {
		swept = append(swept, sweep.kind)
	}
	for _, probe := range listReaderProbes() {
		probed = append(probed, probe.kind)
	}
	sort.Strings(swept)
	sort.Strings(probed)
	assert.Equal(t, swept, probed, "a swept kind whose reader is unprobed can break unnoticed")
}
