package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blang/semver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func newTestServer(t *testing.T, cf clientF) integration.Server {
	t.Helper()
	if cf == nil {
		cf = RealClientF
	}
	prov, err := NewProvider(cf)
	require.NoError(t, err)
	s, err := integration.NewServer(
		context.Background(),
		"mailslurp", semver.Version{Major: 0},
		integration.WithProvider(prov),
	)
	require.NoError(t, err)
	return s
}

// serverWithClient answers a configured provider server that reaches the mock. The server is the
// path the engine takes, and the only path that marshals what an import writes into a program.
func serverWithClient(t *testing.T, mock *MockClient) integration.Server {
	t.Helper()
	s := newTestServer(t, func(context.Context, *Config) (Client, error) { return mock, nil })
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: configMap("k", defaultBaseURL)}))
	return s
}

func configMap(apiKey, endpoint string) property.Map {
	return property.NewMap(map[string]property.Value{
		"apiKey":   property.New(apiKey),
		"endpoint": property.New(endpoint),
	})
}

// The variable is set in this process, so the case that refuses has to clear it first.
func TestConfigureRejectsAnEmptyAPIKey(t *testing.T) {
	t.Setenv(apiKeyEnvironmentVariable, "")
	c := &Config{}
	err := c.Configure(context.Background())
	require.EqualError(t, err,
		"The MailSlurp API key is missing. Set `MAILSLURP_API_KEY` and try again.")
}

// An import stores a default provider holding no configuration, and the engine applies the schema
// default to a program run alone. Refresh and destroy reach Configure empty, so it reads the variable.
func TestConfigureReadsTheAPIKeyFromTheEnvironment(t *testing.T) {
	t.Setenv(apiKeyEnvironmentVariable, "not-a-real-key")
	var got string
	s := newTestServer(t, func(_ context.Context, cfg *Config) (Client, error) {
		got = cfg.APIKey
		return NewMockClient(gomock.NewController(t)), nil
	})

	require.NoError(t, s.Configure(p.ConfigureRequest{
		Args: property.NewMap(nil),
	}))
	assert.Equal(t, "not-a-real-key", got, "Configure built the client without the key of the environment")
}

// The environment answers only for a stack that sets no key. A stack that sets one names the
// account the program means, and a variable of the shell must not send the calls elsewhere.
func TestConfigureKeepsTheConfiguredKeyWhenTheEnvironmentHoldsAnother(t *testing.T) {
	t.Setenv(apiKeyEnvironmentVariable, "the-key-of-the-environment")
	var got string
	s := newTestServer(t, func(_ context.Context, cfg *Config) (Client, error) {
		got = cfg.APIKey
		return NewMockClient(gomock.NewController(t)), nil
	})

	require.NoError(t, s.Configure(p.ConfigureRequest{
		Args: configMap("the-key-of-the-stack", defaultBaseURL),
	}))
	assert.Equal(t, "the-key-of-the-stack", got, "the environment answered for a key the stack sets")
}

// The refusal is the only thing that stands between a stack and a call with no credential, so an
// empty environment and an empty configuration still answer it.
func TestConfigureRefusesWhenNeitherTheConfigurationNorTheEnvironmentHoldsTheKey(t *testing.T) {
	t.Setenv(apiKeyEnvironmentVariable, "")
	built := 0
	s := newTestServer(t, func(context.Context, *Config) (Client, error) {
		built++
		return nil, nil
	})

	err := s.Configure(p.ConfigureRequest{
		Args: property.NewMap(nil),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), missingKeyMessage)
	assert.Equal(t, 0, built, "Configure built a client with no key")
}

func TestConfigureBuildsTheClientOnce(t *testing.T) {
	t.Parallel()
	calls := 0
	c := &Config{APIKey: "k", clientF: func(context.Context, *Config) (Client, error) {
		calls++
		return nil, nil
	}}
	require.NoError(t, c.Configure(context.Background()))
	assert.Equal(t, 1, calls)
}

func TestDiffConfigDoesNotReplaceTheProvider(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)

	old := configMap("old-key", "https://api.mailslurp.com")
	tests := []struct {
		name string
		news property.Map
		want []string
	}{
		{"apiKey change", configMap("new-key", "https://api.mailslurp.com"), []string{"apiKey"}},
		{"endpoint change", configMap("old-key", "https://example.com"), []string{"endpoint"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.DiffConfig(p.DiffRequest{
				State:     old,
				OldInputs: old,
				Inputs:    tt.news,
			})
			require.NoError(t, err)
			assert.True(t, resp.HasChanges)
			for _, key := range tt.want {
				require.Contains(t, resp.DetailedDiff, key)
			}
			for key, d := range resp.DetailedDiff {
				assert.NotEqual(t, p.AddReplace, d.Kind, "property %q", key)
				assert.NotEqual(t, p.UpdateReplace, d.Kind, "property %q", key)
				assert.NotEqual(t, p.DeleteReplace, d.Kind, "property %q", key)
			}
		})
	}
}

func TestDiffConfigReportsNoChangesForEqualConfig(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, nil)
	old := configMap("k", "https://api.mailslurp.com")
	resp, err := s.DiffConfig(p.DiffRequest{State: old, OldInputs: old, Inputs: old})
	require.NoError(t, err)
	assert.False(t, resp.HasChanges)
}

func TestProviderSchemaMarksTheAPIKeySecret(t *testing.T) {
	t.Parallel()
	schema := getSchema(t)
	apiKey := schema.Config.Variables["apiKey"]
	require.NotNil(t, apiKey)
	assert.True(t, apiKey.Secret)
	require.NotNil(t, apiKey.DefaultInfo)
	assert.Contains(t, apiKey.DefaultInfo.Environment, "MAILSLURP_API_KEY")
	assert.NotEmpty(t, apiKey.Description)
	endpoint := schema.Config.Variables["endpoint"]
	require.NotNil(t, endpoint)
	assert.NotEmpty(t, endpoint.Description)
}

func TestRealClientFUsesTheConfiguredEndpointAndKey(t *testing.T) {
	t.Parallel()
	var gotKey, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(InboxDto{ID: "abc", EmailAddress: "a@b.c"})
	}))
	defer ts.Close()

	cl, err := RealClientF(context.Background(), &Config{APIKey: "test-key", Endpoint: ts.URL})
	require.NoError(t, err)
	got, err := cl.GetInbox(context.Background(), "abc")
	require.NoError(t, err)
	assert.Equal(t, "abc", got.ID)
	assert.Equal(t, "test-key", gotKey)
	assert.Equal(t, "/inboxes/abc", gotPath)
}
