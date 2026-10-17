// SPDX-License-Identifier: MPL-2.0

package historybinding

import (
	"testing"

	"github.com/stretchr/testify/require"
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
			id, err := organizationID(test.organizations, test.selection)
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
			endpoint, environment, err := registryDefaults(test.registry)
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
