// SPDX-License-Identifier: MPL-2.0

package historybinding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/composite"
	"github.com/wippyai/runtime/system/registry/history/historytest"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/history/remote"
	"github.com/wippyai/runtime/system/registry/history/remote/remotetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testToken    = "wpy_history_binding_test"
	testRegistry = "https://hub.stage.example.com"
	testTenant   = "11111111-1111-4111-8111-111111111111"
)

type fixture struct {
	server   *remotetest.Server
	dir      string
	source   *memory.Storage
	history  *composite.History
	baseline registry.State
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(bootauth.EnvToken, testToken)
	t.Setenv(bootauth.EnvRegistry, testRegistry)
	server, connection, stop := remotetest.Start()
	t.Cleanup(stop)
	previous := dialRemote
	dialRemote = func(_ context.Context, cfg remote.DialConfig) (*remote.History, error) {
		require.Equal(t, testToken, cfg.Token)
		return remote.New(connection, cfg.Config)
	}
	t.Cleanup(func() { dialRemote = previous })

	source := memory.New()
	v1 := version.FromParent(version.New(0), 1)
	require.NoError(t, source.SaveWithDependencyResolution(v1, historytest.Changes("one", "first"), historytest.Resolution("first", "base"), true))
	require.NoError(t, source.Save(version.FromParent(v1, 2), historytest.Changes("two", "second"), true))
	history := composite.New(source)
	baseline := registry.State{historytest.Entry("base", "value")}
	require.NoError(t, history.SaveBaseline(baseline))
	return &fixture{server: server, dir: dir, source: source, history: history, baseline: baseline}
}

func settings() Settings {
	return Settings{RegistryID: "app", TenantID: testTenant, EnvironmentID: "stage", Endpoint: "history.stage.example.com:443", Timeout: 5 * time.Second}
}

func TestUseRemoteTransfersAndSwitches(t *testing.T) {
	f := setup(t)
	binding, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.NoError(t, err)
	require.Equal(t, BackendRemote, binding.Backend)
	require.Equal(t, testRegistry, binding.Registry)
	require.NotEmpty(t, binding.TransferID)

	remoteHistory, ok := f.history.Active().(*remote.History)
	require.True(t, ok)
	head, err := f.history.Head()
	require.NoError(t, err)
	require.Equal(t, uint(2), head.ID())
	stored, err := f.history.Baseline()
	require.NoError(t, err)
	require.Equal(t, f.baseline[0].ID, stored[0].ID)
	resolution, err := remoteHistory.GetDependencyResolution(version.FromParent(version.New(0), 1))
	require.NoError(t, err)
	require.Equal(t, historytest.Resolution("first", "base").Digest, resolution.Digest)

	v3 := version.FromParent(head, 3)
	require.NoError(t, f.history.Save(v3, historytest.Changes("three", "third"), true))
	_, err = f.source.GetVersion(3)
	require.Error(t, err)

	data, err := os.ReadFile(filepath.Join(f.dir, ".wippy", FileName))
	require.NoError(t, err)
	require.NotContains(t, string(data), testToken)
	loaded, err := Load(f.dir)
	require.NoError(t, err)
	require.Equal(t, binding, loaded)

	reopened, reopenedBinding, err := Open(context.Background(), f.dir, Settings{Timeout: 5 * time.Second})
	require.NoError(t, err)
	require.Equal(t, binding, reopenedBinding)
	reopenedHead, err := reopened.Head()
	require.NoError(t, err)
	require.Equal(t, uint(3), reopenedHead.ID())

	_, err = UseRemote(context.Background(), f.history, f.dir, settings())
	require.ErrorContains(t, err, "already uses remote history")
}

func TestInterruptedUseRemoteResumes(t *testing.T) {
	f := setup(t)
	f.server.Fail = func(method string, after bool) error {
		if method == "CompleteTransfer" && !after {
			return status.Error(codes.Unavailable, "connection lost")
		}
		return nil
	}
	_, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.Error(t, err)
	require.Same(t, f.source, f.history.Active())
	pending, err := Load(f.dir)
	require.NoError(t, err)
	require.Equal(t, BackendLocal, pending.Backend)
	require.NotNil(t, pending.Pending)
	bound, _, err := Open(context.Background(), f.dir, Settings{Timeout: 5 * time.Second})
	require.NoError(t, err)
	require.Nil(t, bound)

	v3 := version.FromParent(version.FromParent(version.FromParent(version.New(0), 1), 2), 3)
	require.NoError(t, f.history.Save(v3, historytest.Changes("three", "third"), true))
	f.server.Fail = nil
	binding, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.NoError(t, err)
	require.Equal(t, pending.Pending.TransferID, binding.TransferID)
	head, err := f.history.Head()
	require.NoError(t, err)
	require.Equal(t, uint(3), head.ID())
	changes, err := f.history.Get(v3)
	require.NoError(t, err)
	require.Equal(t, "three", changes[0].Entry.ID.Name)
}

func TestInterruptedUseRemoteRetrySameSource(t *testing.T) {
	f := setup(t)
	f.server.Fail = func(method string, after bool) error {
		if method == "CompleteTransfer" && !after {
			return status.Error(codes.Unavailable, "connection lost")
		}
		return nil
	}
	_, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.Error(t, err)
	pending, err := Load(f.dir)
	require.NoError(t, err)

	f.server.Fail = nil
	binding, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.NoError(t, err)
	require.Equal(t, pending.Pending.TransferID, binding.TransferID)
	head, err := f.history.Head()
	require.NoError(t, err)
	require.Equal(t, uint(2), head.ID())
}

func TestOpenRequiresCredential(t *testing.T) {
	f := setup(t)
	_, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.NoError(t, err)
	t.Setenv(bootauth.EnvToken, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _, err = Open(context.Background(), f.dir, Settings{Timeout: 5 * time.Second})
	require.ErrorContains(t, err, "history authentication is required")
}

func TestLoadRejectsIncompleteRemoteBinding(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Save(dir, &Binding{Backend: BackendRemote, RegistryID: "app"}))
	_, err := Load(dir)
	require.ErrorContains(t, err, "incomplete")
	require.NoError(t, Save(dir, &Binding{Backend: "other"}))
	_, err = Load(dir)
	require.ErrorContains(t, err, "unknown backend")
	missing, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestUseRemoteStoresRuntimeCredential(t *testing.T) {
	f := setup(t)
	t.Setenv(bootauth.EnvToken, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	bootauth.SetRuntimeToken(testRegistry, testToken)
	t.Cleanup(func() { bootauth.ClearRuntimeToken(testRegistry) })

	_, err := UseRemote(context.Background(), f.history, f.dir, settings())
	require.NoError(t, err)
	bootauth.ClearRuntimeToken(testRegistry)

	credential, err := bootauth.NewStore(bootauth.NewConfig(f.dir)).Get(testRegistry)
	require.NoError(t, err)
	require.Equal(t, testToken, credential.Token)
	reopened, _, err := Open(context.Background(), f.dir, Settings{Timeout: 5 * time.Second})
	require.NoError(t, err)
	require.NotNil(t, reopened)
}
