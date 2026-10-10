// SPDX-License-Identifier: MPL-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/historybinding"
	"github.com/wippyai/runtime/system/registry/history/composite"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historynil "github.com/wippyai/runtime/system/registry/history/nil"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
	"go.uber.org/zap"
)

func TestOpenHistoryWrapsDurableHistory(t *testing.T) {
	t.Chdir(t.TempDir())
	history, closer, err := openHistory(t.Context(), boot.NewConfig(), zap.NewNop())
	require.NoError(t, err)
	selected, ok := history.(*composite.History)
	require.True(t, ok)
	require.IsType(t, &historymem.Storage{}, selected.Active())
	require.Same(t, selected, closer)

	history, closer, err = openHistory(t.Context(), boot.NewConfig(boot.WithSection(RegistryName, map[string]any{RegistryHistoryType: "sqlite"})), zap.NewNop())
	require.NoError(t, err)
	require.IsType(t, &sqlite.History{}, history.(*composite.History).Active())
	require.NoError(t, closer.Close())

	history, closer, err = openHistory(t.Context(), boot.NewConfig(boot.WithSection(RegistryName, map[string]any{RegistryEnableHistory: false})), zap.NewNop())
	require.NoError(t, err)
	require.IsType(t, &historynil.History{}, history)
	require.Nil(t, closer)
}

func TestOpenHistoryUsesRemoteBindingWithoutFallback(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(bootauth.EnvToken, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, historybinding.Save(dir, &historybinding.Binding{
		Backend: historybinding.BackendRemote, Registry: "https://hub.stage.example.com", Endpoint: "history.stage.example.com:443",
		TenantID: "11111111-1111-4111-8111-111111111111", EnvironmentID: "stage", RegistryID: "app", TransferID: "transfer",
	}))
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{RegistryHistoryType: "sqlite"}))
	_, _, err := openHistory(t.Context(), cfg, zap.NewNop())
	require.ErrorContains(t, err, "history authentication is required")
}
