// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// An image that changes the locked version of a recorded module ships only
// the new artifact. Offline startup must boot the new lock, not restore the
// recorded version that the image no longer holds.
func TestDeploymentLockVersionChangeBootsOfflineWithoutRecordedArtifact(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, checkout := range []bool{false, true} {
			for _, overlay := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/checkout=%t/overlay=%t", engine, checkout, overlay), func(t *testing.T) {
					fixture := newDeploymentRestartFixture(t)
					if checkout {
						fixture.sourceCheckout(t)
					}
					openHistory := deploymentRestartHistory(t, engine)
					history, closeHistory := openHistory()
					ctx, reg, _ := fixture.registry(t, history, false)
					root, err := history.Head()
					require.NoError(t, err)
					require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
					head := root
					switch {
					case overlay && checkout:
						worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
						require.NoError(t, err)
						worker.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
						head, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
						require.NoError(t, err)
					case overlay:
						head = fixture.update(ctx, t, reg)
					default:
						head, err = history.Head()
						require.NoError(t, err)
					}
					require.Equal(t, "1.0.0", lockModuleVersion(t, fixture.lockPath, "acme/worker"))
					closeHistory()

					fixture.shipOnly(t, "worker", "2.0.0")

					for i := 0; i < 2; i++ {
						history, closeHistory = openHistory()
						ctx, restarted, calls := fixture.registry(t, history, true)
						require.NoError(t, restarted.LoadState(ctx, fixture.baseline, head), "offline restart %d", i)
						require.Zero(t, *calls, "restart must not consult the Hub")
						require.Equal(t, "2.0.0", resolutionModuleVersion(restarted.Snapshot().Registry.Resolution, "acme/worker"))
						closeHistory()
					}
				})
			}
		}
	}
}

// A Hub overlay that the lock does not pin still needs its own artifact. If it
// is lost, offline startup fails and leaves the recorded history intact.
func TestDeploymentOverlayWithoutArtifactStillFailsOffline(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, checkout := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/checkout=%t", engine, checkout), func(t *testing.T) {
				fixture := newDeploymentRestartFixture(t)
				if checkout {
					fixture.sourceCheckout(t)
				}
				history, closeHistory := deploymentRestartHistory(t, engine)()
				defer closeHistory()
				ctx, reg, _ := fixture.registry(t, history, false)
				root, err := history.Head()
				require.NoError(t, err)
				require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
				worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
				require.NoError(t, err)
				worker.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
				head, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
				require.NoError(t, err)
				require.Equal(t, "2.0.0", resolutionModuleVersion(reg.Snapshot().Registry.Resolution, "acme/worker"))
				before, err := history.(regapi.ResolutionHistory).GetDependencyResolution(head)
				require.NoError(t, err)
				fixture.shipOnly(t, "worker", "1.0.0")

				ctx = regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
				err = loadOffline(ctx, t, fixture, history, head)
				require.ErrorContains(t, err, "verified dependency evidence is unavailable during offline startup")
				after, err := history.(regapi.ResolutionHistory).GetDependencyResolution(head)
				require.NoError(t, err)
				require.Equal(t, before, after)
				current, err := history.Head()
				require.NoError(t, err)
				require.Equal(t, head.ID(), current.ID())
			})
		}
	}
}

func loadOffline(ctx context.Context, t *testing.T, fixture *deploymentRestartFixture, history regapi.History, head regapi.Version) error {
	t.Helper()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{}, Resolver: topology.NewResolver(), Logger: zap.NewNop(), LockPath: fixture.lockPath, VendorDir: fixture.vendorDir,
	})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	if err := handler.PrepareRestore(ctx, history); err != nil {
		return err
	}
	resolver := topology.NewResolver()
	reg := registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
		registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(handler.Expand).
			WithChangesExpansion(handler.ExpandChanges).WithResolutionTransition(handler.ReconcileResolution)))
	return reg.LoadState(regapi.WithRegistry(ctx, reg), fixture.baseline, head)
}

// sourceCheckout turns the fixture into an unrooted lock with host-owned
// declarations, as an application image ships it.
func (f *deploymentRestartFixture) sourceCheckout(t *testing.T) {
	t.Helper()
	l, err := lock.New(f.lockPath)
	require.NoError(t, err)
	l.SetRootModule("")
	require.NoError(t, l.Write())
	for i := range f.baseline {
		if f.baseline[i].Kind == regapi.NamespaceDependency {
			f.baseline[i].Registry.Owner = ""
		}
	}
}

// shipOnly replaces every local artifact of a module with one version, as a
// new image does, and pins that version in the lock.
func (f *deploymentRestartFixture) shipOnly(t *testing.T, module, version string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(f.vendorDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.Contains(path, module) {
			return err
		}
		return os.Remove(path)
	}))
	key := module + "@" + version
	require.NoError(t, os.WriteFile(filepath.Join(f.vendorDir, "acme", module+"-"+version+".wapp"), f.artifacts[key], 0o600))
	l, err := lock.New(f.lockPath)
	require.NoError(t, err)
	l.SetModule(lock.Module{Name: "acme/" + module, Version: version, Hash: strings.TrimPrefix(f.digests[key], "sha256:")})
	require.NoError(t, l.Write())
}

func lockModuleVersion(t *testing.T, path, name string) string {
	t.Helper()
	l, err := lock.New(path)
	require.NoError(t, err)
	for _, module := range l.GetModules() {
		if module.Name == name {
			return module.Version
		}
	}
	return ""
}
