// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
)

// unreachableHub fails the test if an operation without network entitlement
// reaches the Hub through any method.
type unreachableHub struct {
	t *testing.T
}

func (h unreachableHub) GetManifest(context.Context, string, string, string) (*ModuleManifest, error) {
	h.t.Fatal("offline operation reached the Hub through GetManifest")
	return nil, nil
}

func (h unreachableHub) ListAllVersions(context.Context, string, string) ([]VersionInfo, error) {
	h.t.Fatal("offline operation reached the Hub through ListAllVersions")
	return nil, nil
}

func (h unreachableHub) GetDownloadURL(context.Context, *DownloadParams) (*DownloadInfo, error) {
	h.t.Fatal("offline operation reached the Hub through GetDownloadURL")
	return nil, nil
}

func (h unreachableHub) DownloadToFile(context.Context, string, string) error {
	h.t.Fatal("offline operation reached the Hub through DownloadToFile")
	return nil
}

func TestHubForWithholdsClientWithoutNetworkEntitlement(t *testing.T) {
	handler := &DependencyHandler{hub: unreachableHub{t: t}}

	for _, test := range []struct {
		ctx   context.Context
		name  string
		grant bool
	}{
		{name: "no policy stated", ctx: context.Background()},
		{name: "explicitly offline", ctx: regapi.WithDependencyAccess(context.Background(), regapi.DependencyAccessVerifiedOffline)},
		{name: "explicitly online", ctx: regapi.WithDependencyAccess(context.Background(), regapi.DependencyAccessOnline), grant: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := handler.hubFor(test.ctx)
			if test.grant {
				require.NotEqual(t, offlineHubClient{}, client, "granted operation must hold the live client")
				return
			}
			require.Equal(t, offlineHubClient{}, client, "ungranted operation must not hold the live client")
		})
	}
}

func TestOfflineHubClientRefusesEveryNetworkMethod(t *testing.T) {
	client := offlineHubClient{}
	ctx := context.Background()

	_, manifestErr := client.GetManifest(ctx, "acme", "worker", "1.0.0")
	requireOfflineRefusal(t, manifestErr, "resolve manifest", "acme/worker")

	_, versionsErr := client.ListAllVersions(ctx, "acme", "worker")
	requireOfflineRefusal(t, versionsErr, "list versions", "acme/worker")

	_, downloadURLErr := client.GetDownloadURL(ctx, &DownloadParams{Org: "acme", Module: "worker"})
	requireOfflineRefusal(t, downloadURLErr, "fetch artifact metadata", "acme/worker")

	requireOfflineRefusal(t, client.DownloadToFile(ctx, "memory://worker", t.TempDir()+"/out"), "download artifact", "")
}

// requireOfflineRefusal asserts the refusal keeps the detail a user acts on.
func requireOfflineRefusal(t *testing.T, err error, operation, module string) {
	t.Helper()
	var refusal apierror.Error
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, apierror.False, refusal.Retryable())

	details := refusal.Details()
	require.NotNil(t, details)
	gotOperation, ok := details.Get("operation")
	require.True(t, ok)
	require.Equal(t, operation, gotOperation)

	hint, ok := details.Get("hint")
	require.True(t, ok)
	require.Contains(t, hint, "wippy update/install")

	gotModule, ok := details.Get("module")
	if module == "" {
		require.False(t, ok, "an unattributed refusal must not invent a module")
		return
	}
	require.True(t, ok)
	require.Equal(t, module, gotModule)
}

func TestHubForWithholdsClientFromNilHandler(t *testing.T) {
	var handler *DependencyHandler
	require.Equal(t, offlineHubClient{}, handler.hubFor(context.Background()))
}
