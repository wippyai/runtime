// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"github.com/wippyai/runtime/system/registry/history/composite"
	"github.com/wippyai/runtime/system/registry/history/remote"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestHistoryBackendRequiresPermission(t *testing.T) {
	ctx, release := strictOverlayContext(t)
	defer release()
	runOverlayLua(ctx, t, overlayTestRegistry("owner", nil), `
		local backend, err = registry.history_backend()
		assert(backend == nil and err:kind() == errors.PERMISSION_DENIED)
		local selected, selectErr = registry.use_remote_history({name = "app"})
		assert(selected == nil and selectErr:kind() == errors.PERMISSION_DENIED)
	`)
}

type historyRegistry struct {
	*mockRegistry
	history regapi.History
}

func (r historyRegistry) History() regapi.History { return r.history }

func TestHistoryBackendReportsActiveDriver(t *testing.T) {
	ctx, release := strictOverlayContext(t, "registry.history.get\x00")
	defer release()
	runOverlayLua(ctx, t, overlayTestRegistry("owner", nil), `
		local backend, err = registry.history_backend()
		assert(err == nil and backend.backend == "local" and backend.name == nil)
	`)
	connection, err := grpc.NewClient("passthrough:///history", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer connection.Close()
	remoteHistory, err := remote.New(connection, remote.Config{Key: &historyv1.RegistryKey{TenantId: "11111111-1111-4111-8111-111111111111", EnvironmentId: "stage", RegistryId: "app"}, Timeout: time.Second})
	require.NoError(t, err)
	remoteCtx, releaseRemote := strictOverlayContext(t, "registry.history.get\x00")
	defer releaseRemote()
	reg := historyRegistry{mockRegistry: overlayTestRegistry("owner", nil), history: composite.New(remoteHistory)}
	l := lua.NewState()
	defer l.Close()
	l.SetContext(regapi.WithRegistry(remoteCtx, reg))
	lua.OpenErrors(l)
	setupModule(l)
	require.NoError(t, l.DoString(`
		local backend, err = registry.history_backend()
		assert(err == nil and backend.backend == "remote" and backend.name == "app")
		assert(backend.organization_id == "11111111-1111-4111-8111-111111111111" and backend.environment == "stage")
	`))
}

func TestUseRemoteHistoryValidatesRequest(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, release := strictOverlayContext(t, "registry.history.select\x00app")
	defer release()
	runOverlayLua(ctx, t, overlayTestRegistry("owner", nil), `
		local missing, missingErr = registry.use_remote_history({})
		assert(missing == nil and missingErr:kind() == errors.INVALID)
		local selected, err = registry.use_remote_history({name = "app"})
		assert(selected == nil and err:kind() == errors.INVALID)
		assert(tostring(err):find("cannot be switched"))
	`)
}
