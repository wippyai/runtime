// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/historybinding"
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

func TestHistoryBackendReadsBinding(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ctx, release := strictOverlayContext(t, "registry.history.get\x00")
	defer release()
	runOverlayLua(ctx, t, overlayTestRegistry("owner", nil), `
		local backend, err = registry.history_backend()
		assert(err == nil and backend.backend == "local" and backend.name == nil)
	`)
	require.NoError(t, historybinding.Save(dir, &historybinding.Binding{
		Backend: historybinding.BackendRemote, Registry: "https://hub.stage.example.com", Endpoint: "history.stage.example.com:443",
		TenantID: "11111111-1111-4111-8111-111111111111", EnvironmentID: "stage", RegistryID: "app", TransferID: "transfer",
	}))
	runOverlayLua(ctx, t, overlayTestRegistry("owner", nil), `
		local backend, err = registry.history_backend()
		assert(err == nil and backend.backend == "remote" and backend.name == "app")
		assert(backend.organization_id == "11111111-1111-4111-8111-111111111111" and backend.environment == "stage")
		assert(backend.transfer_id == nil)
	`)
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
