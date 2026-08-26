package examples

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	"github.com/jschady/pulumi-mailslurp/provider"
)

// TestTheAttachedProviderAnswersACallAfterTheEngineCancelled pins why the attached server ignores
// Cancel. A leg runs preview, up, refresh and destroy as separate pulumi processes against one
// server, and each process sends Cancel when it ends. Without the wrapper the second process finds
// a provider whose every context is already cancelled.
func TestTheAttachedProviderAnswersACallAfterTheEngineCancelled(t *testing.T) {
	var served atomic.Int32
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"inbox-1","emailAddress":"inbox-1@example.com"}`))
	}))
	defer vendor.Close()

	raw, err := provider.NewWith(http.DefaultTransport)(nil)
	require.NoError(t, err)
	server := outlivesTheEngine{raw}

	ctx := context.Background()
	config, err := structpb.NewStruct(map[string]any{"apiKey": "placeholder", "endpoint": vendor.URL})
	require.NoError(t, err)
	_, err = server.Configure(ctx, &rpc.ConfigureRequest{Args: config, AcceptSecrets: true})
	require.NoError(t, err)

	_, err = server.Cancel(ctx, &emptypb.Empty{})
	require.NoError(t, err)

	args, err := structpb.NewStruct(map[string]any{"inboxId": "inbox-1"})
	require.NoError(t, err)
	_, err = server.Invoke(ctx, &rpc.InvokeRequest{Tok: "mailslurp:index:getInbox", Args: args})
	if err != nil {
		require.NotContains(t, err.Error(), "context canceled", "the call after Cancel must reach the vendor")
	}
	require.EqualValues(t, 1, served.Load(), "the vendor should answer the call that follows the Cancel")
}
