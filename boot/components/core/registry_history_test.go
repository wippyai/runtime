package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authapi "github.com/wippyai/runtime/api/auth"
	"github.com/wippyai/runtime/api/boot"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
)

func TestHistoryOrganizationSelection(t *testing.T) {
	const firstID = "11111111-1111-4111-8111-111111111111"
	const secondID = "22222222-2222-4222-8222-222222222222"
	organizations := []bootauth.OrgInfo{{ID: firstID, Name: "first"}, {ID: secondID, Name: "second"}}
	for _, test := range []struct {
		name          string
		selection     string
		want          string
		error         string
		organizations []bootauth.OrgInfo
	}{
		{name: "single", organizations: organizations[:1], want: firstID},
		{name: "selected", organizations: organizations, selection: "second", want: secondID},
		{name: "multiple", organizations: organizations, error: "history_organization"},
		{name: "unknown", organizations: organizations, selection: "missing", error: "not available"},
		{name: "empty", error: "no organizations"},
		{name: "invalid identity", organizations: []bootauth.OrgInfo{{ID: "invalid", Name: "first"}}, error: "invalid organization identity"},
		{name: "zero identity", organizations: []bootauth.OrgInfo{{ID: "00000000-0000-0000-0000-000000000000", Name: "first"}}, error: "invalid organization identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, err := historyOrganizationID(test.organizations, test.selection)
			if test.error != "" {
				require.ErrorContains(t, err, test.error)
				require.Empty(t, id)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, id)
		})
	}
}

func TestHistoryOrganizationCredentials(t *testing.T) {
	const token = "wpy_history_organization_test"
	const firstID = "11111111-1111-4111-8111-111111111111"
	const secondID = "22222222-2222-4222-8222-222222222222"
	for _, source := range []string{"environment", "login", "file", "project", "override", "legacy", "unreadable file", "empty file", "invalid project", "conflicting selection", "denied", "timeout", "invalid timeout"} {
		t.Run(source, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			t.Setenv(bootauth.EnvToken, token)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
				require.Equal(t, "/api/v1/account/orgs", r.URL.Path)
				if source == "denied" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if source == "timeout" {
					<-r.Context().Done()
					return
				}
				orgs := []map[string]any{{"org": map[string]string{"id": firstID, "name": "first"}}}
				if source == "project" || source == "override" {
					orgs = append(orgs, map[string]any{"org": map[string]string{"id": secondID, "name": "second"}})
				}
				require.NoError(t, json.NewEncoder(w).Encode(orgs))
			}))
			defer server.Close()
			t.Setenv(bootauth.EnvRegistry, server.URL)
			settings := map[string]any{"history_endpoint": "history.example.com:443", "history_environment_id": "test", "history_registry_id": "app"}
			wantID, wantError := firstID, ""
			wantRequests := int32(1)
			switch source {
			case "login":
				t.Setenv(bootauth.EnvToken, "")
				store := bootauth.NewStore(bootauth.NewConfig(directory))
				require.NoError(t, store.Set(&authapi.Credential{Token: token, Registry: server.URL}, false))
			case "file", "unreadable file", "empty file":
				path := filepath.Join(directory, "token")
				settings["history_token_file"] = path
				switch source {
				case "file":
					require.NoError(t, os.WriteFile(path, []byte(token), 0600))
					t.Setenv(bootauth.EnvToken, "wrong-default-token")
				case "empty file":
					require.NoError(t, os.WriteFile(path, nil, 0600))
					wantError, wantRequests = "token file is empty", 0
				default:
					wantError, wantRequests = "read history token", 0
				}
			case "project", "override":
				require.NoError(t, os.WriteFile(filepath.Join(directory, "wippy.yaml"), []byte("organization: second\nmodule: app\n"), 0600))
				wantID = secondID
				if source == "override" {
					settings["history_organization"] = "first"
					wantID = firstID
				}
			case "legacy":
				settings["history_tenant_id"] = firstID
				wantRequests = 0
			case "invalid project":
				require.NoError(t, os.WriteFile(filepath.Join(directory, "wippy.yaml"), []byte("[invalid"), 0600))
				wantError, wantRequests = "read history organization from project", 0
			case "conflicting selection":
				settings["history_tenant_id"] = firstID
				settings["history_organization"] = "second"
				wantError, wantRequests = "not both", 0
			case "denied":
				wantError = "resolve history organization"
			case "timeout":
				settings["history_timeout"] = 100 * time.Millisecond
				wantError = "context deadline exceeded"
			case "invalid timeout":
				settings["history_timeout"] = time.Duration(0)
				wantError, wantRequests = "history timeout must be positive", 0
			}
			cfg := boot.NewConfig(boot.WithSection(RegistryName, settings))
			dial, err := historyConnectionConfig(t.Context(), cfg.Sub(RegistryName))
			if wantError != "" {
				require.ErrorContains(t, err, wantError)
			} else {
				require.NoError(t, err)
				require.Equal(t, token, dial.Token)
				require.Empty(t, dial.TokenFile)
				require.Equal(t, wantID, dial.Key.TenantId)
				require.Equal(t, "test", dial.Key.EnvironmentId)
				require.Equal(t, "app", dial.Key.RegistryId)
			}
			require.Equal(t, wantRequests, requests.Load())
		})
	}
}

func TestHistoryRegistryDefaults(t *testing.T) {
	for _, test := range []struct {
		registry    string
		endpoint    string
		environment string
	}{
		{registry: "https://hub.preview.example.com", endpoint: "history.preview.example.com:443", environment: "preview"},
		{registry: "https://hub.example.com/", endpoint: "history.example.com:443", environment: "example"},
		{registry: "https://hub.example.com:8443", endpoint: "history.example.com:8443", environment: "example"},
		{registry: "https://HUB.PREVIEW.EXAMPLE.COM", endpoint: "history.preview.example.com:443", environment: "preview"},
		{registry: "https://hub.preview-2.example.com", endpoint: "history.preview-2.example.com:443", environment: "preview-2"},
		{registry: "http://hub.example.com"},
		{registry: "https://registry.example.com"},
		{registry: "https://hub.example.com/path"},
		{registry: "https://hub.example.com?query=value"},
		{registry: "https://hub.example.com?"},
		{registry: "https://hub.example.com#fragment"},
		{registry: "https://user@hub.example.com"},
		{registry: "https://hub.example.com:0"},
		{registry: "https://hub.example.com:65536"},
		{registry: "https://hub.example.com:port"},
		{registry: "https://hub.example.com:"},
		{registry: "https://hub..example.com"},
		{registry: "https://hub.-example.com"},
		{registry: "https://hub.example-.com"},
		{registry: "https://hub.example_.com"},
		{registry: "https://hub.example"},
		{registry: "https://hub.example.com."},
		{registry: "hub.example.com"},
		{registry: "https://127.0.0.1"},
		{registry: "https://[::1]"},
		{registry: "%"},
	} {
		t.Run(test.registry, func(t *testing.T) {
			endpoint, environment, err := historyRegistryDefaults(test.registry)
			if test.endpoint == "" {
				require.ErrorContains(t, err, "cannot derive History settings")
				require.Empty(t, endpoint)
				require.Empty(t, environment)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.endpoint, endpoint)
			require.Equal(t, test.environment, environment)
		})
	}
}

func TestHistoryConnectionDefaults(t *testing.T) {
	for _, source := range []string{"environment", "login", "endpoint override", "environment override", "explicit settings", "legacy file", "invalid registry"} {
		t.Run(source, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			const registry = "https://hub.preview.example.com"
			const token = "wpy_history_defaults_test"
			t.Setenv(bootauth.EnvRegistry, registry)
			t.Setenv(bootauth.EnvToken, token)
			settings := map[string]any{"history_registry_id": "app", "history_tenant_id": "11111111-1111-4111-8111-111111111111"}
			endpoint, environment := "history.preview.example.com:443", "preview"
			switch source {
			case "login":
				t.Setenv(bootauth.EnvToken, "")
				store := bootauth.NewStore(bootauth.NewConfig(directory))
				require.NoError(t, store.Set(&authapi.Credential{Token: token, Registry: registry}, false))
			case "endpoint override":
				endpoint = "custom.example.com:8443"
				settings["history_endpoint"] = endpoint
			case "environment override":
				environment = "custom"
				settings["history_environment_id"] = environment
			case "explicit settings":
				t.Setenv(bootauth.EnvRegistry, "https://registry.example.com")
				endpoint, environment = "custom.example.com:8443", "custom"
				settings["history_endpoint"], settings["history_environment_id"] = endpoint, environment
			case "legacy file":
				t.Setenv(bootauth.EnvToken, "")
				settings["history_token_file"] = filepath.Join(directory, "token")
			case "invalid registry":
				t.Setenv(bootauth.EnvRegistry, "http://hub.example.com")
			}
			cfg := boot.NewConfig(boot.WithSection(RegistryName, settings))
			dial, err := historyConnectionConfig(t.Context(), cfg.Sub(RegistryName))
			if source == "invalid registry" {
				require.ErrorContains(t, err, "cannot derive History settings")
				require.Empty(t, dial.Token)
				return
			}
			require.NoError(t, err)
			require.Equal(t, endpoint, dial.Endpoint)
			require.Equal(t, environment, dial.Key.EnvironmentId)
			require.Equal(t, "app", dial.Key.RegistryId)
			if source == "legacy file" {
				require.Equal(t, settings["history_token_file"], dial.TokenFile)
				require.Empty(t, dial.Token)
			} else {
				require.Equal(t, token, dial.Token)
			}
		})
	}
}
