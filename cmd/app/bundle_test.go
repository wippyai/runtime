// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func testBundle(t *testing.T) Bundle {
	t.Helper()
	var data bytes.Buffer
	err := wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "acme.app", "name": "app", "version": "1.0.0"}, nil, &data)
	require.NoError(t, err)
	digest := sha256.Sum256(data.Bytes())
	return Bundle{Root: "acme/app", Packs: []Pack{{Module: "acme/app", Version: "1.0.0", Digest: fmt.Sprintf("sha256:%x", digest), Data: data.Bytes()}}}
}

func TestBundleSeedsOfflineAndPreservesUpdatedVersion(t *testing.T) {
	bundle := testBundle(t)
	dir := filepath.Join(t.TempDir(), "deployment")
	path, err := bundle.Seed(dir)
	require.NoError(t, err)
	locked, err := lock.New(path)
	require.NoError(t, err)
	require.Equal(t, []string{"acme/app"}, locked.GetRootModules())
	paths := locked.GetModuleLoadPaths()
	require.Len(t, paths, 2)
	data, err := os.ReadFile(paths[1].Path)
	require.NoError(t, err)
	require.Equal(t, bundle.Packs[0].Data, data)
	locked.SetModule(lock.Module{Name: "acme/app", Version: "2.0.0", Root: true})
	require.NoError(t, locked.Write())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = bundle.Seed(dir)
	require.NoError(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestBundleRejectsCorruptionAndIdentityMismatch(t *testing.T) {
	for _, mutation := range []func(*Bundle){
		func(b *Bundle) { b.Packs[0].Data = append(b.Packs[0].Data, 0) },
		func(b *Bundle) { b.Packs[0].Version = "2.0.0" },
		func(b *Bundle) { b.Packs = append(b.Packs, b.Packs[0]) },
		func(b *Bundle) { b.Root = "acme/other" },
	} {
		bundle := testBundle(t)
		mutation(&bundle)
		dir := filepath.Join(t.TempDir(), "deployment")
		_, err := bundle.Seed(dir)
		require.Error(t, err)
		_, err = os.Stat(dir)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestBundleFailsClosedOnExistingInvalidDeployment(t *testing.T) {
	bundle := testBundle(t)
	dir := t.TempDir()
	_, err := bundle.Seed(dir)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wippy.lock"), []byte("not: [yaml"), 0o600))
	_, err = bundle.Seed(dir)
	require.Error(t, err)
}

func TestBundleConcurrentFirstLaunch(t *testing.T) {
	bundle := testBundle(t)
	dir := filepath.Join(t.TempDir(), "deployment")
	var wait sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wait.Go(func() { _, err := bundle.Seed(dir); failures <- err })
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
}

func bundleWithMarker(t *testing.T, marker string) Bundle {
	t.Helper()
	var data bytes.Buffer
	err := wapp.NewWriter().PackEntries(
		wapp.Metadata{"namespace": "acme.app", "name": "app", "version": "1.0.0"},
		[]wapp.Entry{{ID: wapp.NewID("acme.app", "marker"), Kind: "ns.definition", Data: map[string]any{"marker": marker}}},
		&data,
	)
	require.NoError(t, err)
	digest := sha256.Sum256(data.Bytes())
	return Bundle{Root: "acme/app", Packs: []Pack{{Module: "acme/app", Version: "1.0.0", Digest: fmt.Sprintf("sha256:%x", digest), Data: data.Bytes()}}}
}

func TestChangedBundleSeedsRetainedImmutableOverlayOffline(t *testing.T) {
	state := t.TempDir()
	oldBundle := bundleWithMarker(t, "old")
	oldDeployment := embeddedDeployment(state, oldBundle)
	_, err := oldBundle.Seed(oldDeployment)
	require.NoError(t, err)
	oldLockBefore, err := os.ReadFile(filepath.Join(oldDeployment, lock.DefaultFilename))
	require.NoError(t, err)

	// An installed overlay is not necessarily listed in the embedded bundle's
	// lock. It is still a retained immutable artifact and must be carried over.
	overlay := []byte("retained overlay artifact")
	overlayDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(overlay))
	overlayName, err := graph.ParseName("wippy/agent")
	require.NoError(t, err)
	overlayPath := filepath.Join(oldDeployment, ".wippy", "vendor", immutableRelativePath(overlayName, "0.1.0-dev", overlayDigest))
	require.NotEmpty(t, overlayPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(overlayPath), 0o700))
	require.NoError(t, os.WriteFile(overlayPath, overlay, 0o600))

	newBundle := bundleWithMarker(t, "new")
	newDeployment := embeddedDeployment(state, newBundle)
	_, err = newBundle.Seed(newDeployment)
	require.NoError(t, err)
	require.NoError(t, seedDependencyCache(state, newDeployment, newBundle))

	cache := dependencyVendorDirectory(state)
	require.NoError(t, verifyCachedPath(cache, immutableRelativePath(graph.MustParseName("acme/app"), "1.0.0", newBundle.Packs[0].Digest), newBundle.Packs[0].Digest, uint64(len(newBundle.Packs[0].Data))))
	retainedRelative := immutableRelativePath(overlayName, "0.1.0-dev", overlayDigest)
	require.NoError(t, verifyCachedPath(cache, retainedRelative, overlayDigest, uint64(len(overlay))))

	oldLock, err := os.ReadFile(filepath.Join(oldDeployment, lock.DefaultFilename))
	require.NoError(t, err)
	require.Equal(t, oldLockBefore, oldLock)
}

func TestDependencyCacheRejectsRetainedSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	state := t.TempDir()
	bundle := bundleWithMarker(t, "stable")
	deployment := embeddedDeployment(state, bundle)
	_, err := bundle.Seed(deployment)
	require.NoError(t, err)
	outside := filepath.Join(t.TempDir(), "outside.wapp")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	symlink := filepath.Join(deployment, ".wippy", "vendor", "wippy", "agent-0.1.0-dev.sha256-"+fmt.Sprintf("%x", sha256.Sum256([]byte("outside")))+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(symlink), 0o700))
	require.NoError(t, os.Symlink(outside, symlink))
	require.ErrorContains(t, seedDependencyCache(state, deployment, bundle), "symlink")
}

type cacheOfflineHub struct{ requests int }

func (h *cacheOfflineHub) GetManifest(context.Context, string, string, string) (*hub.ModuleManifest, error) {
	h.requests++
	return nil, fmt.Errorf("Hub must not be contacted during restore")
}

func (h *cacheOfflineHub) ListAllVersions(context.Context, string, string) ([]hub.VersionInfo, error) {
	h.requests++
	return nil, fmt.Errorf("Hub must not be contacted during restore")
}

func (h *cacheOfflineHub) GetDownloadURL(context.Context, *hub.DownloadParams) (*hub.DownloadInfo, error) {
	h.requests++
	return nil, fmt.Errorf("Hub must not be contacted during restore")
}

func (h *cacheOfflineHub) DownloadToFile(context.Context, string, string) error {
	h.requests++
	return fmt.Errorf("Hub must not be contacted during restore")
}

func TestChangedBundleRestoresHistoricalOverlayOffline(t *testing.T) {
	state := t.TempDir()
	oldBundle := bundleWithMarker(t, "old")
	oldDeployment := embeddedDeployment(state, oldBundle)
	_, err := oldBundle.Seed(oldDeployment)
	require.NoError(t, err)
	newBundle := bundleWithMarker(t, "new")
	newDeployment := embeddedDeployment(state, newBundle)
	newLock, err := newBundle.Seed(newDeployment)
	require.NoError(t, err)

	overlay := []byte("retained overlay artifact")
	overlayDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(overlay))
	overlayRelative := immutableRelativePath(graph.MustParseName("wippy/agent"), "0.1.0-dev", overlayDigest)
	overlayPath := filepath.Join(oldDeployment, ".wippy", "vendor", overlayRelative)
	require.NoError(t, os.MkdirAll(filepath.Dir(overlayPath), 0o700))
	require.NoError(t, os.WriteFile(overlayPath, overlay, 0o600))
	require.NoError(t, seedDependencyCache(state, newDeployment, newBundle))

	oldResolution := (&regapi.DependencyResolution{
		Deployment: &regapi.Deployment{Root: "acme/app", Modules: []regapi.ResolvedModule{{
			Name: "acme/app", Version: "1.0.0", Source: "hub", Digest: oldBundle.Packs[0].Digest,
		}}},
		Modules: []regapi.ResolvedModule{
			{Name: "acme/app", Version: "1.0.0", Source: "hub", Digest: oldBundle.Packs[0].Digest},
			{Name: "wippy/agent", Version: "0.1.0-dev", Source: "hub", Digest: overlayDigest},
		},
	}).Canonical()
	history := historymem.New()
	root, err := history.GetVersion(regapi.RootVersion)
	require.NoError(t, err)
	head := version.FromParent(root, 1)
	require.NoError(t, history.SaveWithDependencyResolution(head, nil, oldResolution, true))

	client := &cacheOfflineHub{}
	depHandler, err := hub.NewDependencyHandler(hub.DependencyHandlerOptions{
		Hub: client, Logger: zap.NewNop(), LockPath: newLock,
		VendorDir: dependencyVendorDirectory(state),
	})
	require.NoError(t, err)
	ctx := moduleapi.WithSourceRegistry(ctxapi.NewRootContext(), moduleapi.NewSourceRegistry())
	ctx = regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline)
	require.NoError(t, depHandler.PrepareRestore(ctx, history))
	require.Zero(t, client.requests)
	sources := moduleapi.GetSourceRegistry(ctx).Snapshot()
	require.Equal(t, newBundle.Packs[0].Digest, sources["acme/app"].Digest)
}
