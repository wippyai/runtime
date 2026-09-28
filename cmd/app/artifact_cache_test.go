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
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// bundleWithMarker builds a bundle whose single pack differs by marker, so two
// executables select distinct deployments in the same state.
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

// retainRow adds a module row to a retained deployment lock without touching
// the artifacts it points at, reproducing a lock a previous runtime left.
func retainRow(t *testing.T, deployment string, module lock.Module) {
	t.Helper()
	locked, err := lock.New(filepath.Join(deployment, lock.DefaultFilename))
	require.NoError(t, err)
	locked.SetModule(module)
	require.NoError(t, locked.Write())
}

// seededState seeds a state whose selected deployment carries bundle.
func seededState(t *testing.T) (string, string, Bundle) {
	t.Helper()
	state := t.TempDir()
	bundle := bundleWithMarker(t, "stable")
	deployment := filepath.Join(deploymentsPath(state), bundle.ID())
	_, err := bundle.Seed(deployment)
	require.NoError(t, err)
	return state, deployment, bundle
}

// immutableRelative names a cache entry through the same contract production
// code publishes it under.
func immutableRelative(t *testing.T, module, version, digest string) string {
	t.Helper()
	_, relative, err := immutableArtifactPath(module, version, digest)
	require.NoError(t, err)
	return relative
}

func legacyArtifact(t *testing.T, state, content string) (string, string) {
	t.Helper()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
	relative := immutableRelative(t, "wippy/agent", "0.1.0-dev", digest)
	path := filepath.Join(legacyArtifactVendorPath(state), relative)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return relative, digest
}

func TestSeedCacheImportsVerifiedLegacyArtifacts(t *testing.T) {
	state, deployment, bundle := seededState(t)
	relative, digest := legacyArtifact(t, state, "legacy artifact")

	_, err := seedCache(state, deployment, bundle)
	require.NoError(t, err)
	require.NoError(t, hub.VerifyDownloadedArtifact(filepath.Join(cachePath(state), relative), digest, 0))
	require.FileExists(t, filepath.Join(legacyArtifactVendorPath(state), relative))
}

func TestImportLegacyArtifactCacheRejectsCorruptContent(t *testing.T) {
	state := t.TempDir()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("expected artifact")))
	relative := immutableRelative(t, "wippy/agent", "0.1.0-dev", digest)
	path := filepath.Join(legacyArtifactVendorPath(state), relative)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("corrupt artifact"), 0o600))

	err := importLegacyArtifactCache(state, cachePath(state))
	require.Error(t, err)
	require.ErrorContains(t, err, "digest")
}

func TestImportLegacyArtifactCacheRejectsSymlinkBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	state := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(state, legacyCacheDir)))

	err := importLegacyArtifactCache(state, cachePath(state))
	require.Error(t, err)
	require.ErrorContains(t, err, "legacy artifact cache")
}

// retainedOverlay writes an immutable artifact into a retained deployment
// vendor and returns its cache-relative path and digest.
func retainedOverlay(t *testing.T, deployment, content string) (string, string) {
	t.Helper()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
	relative := immutableRelative(t, "wippy/agent", "0.1.0-dev", digest)
	path := filepath.Join(deployment, ".wippy", "vendor", relative)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return relative, digest
}

// damagedDeployment seeds a retained deployment and then corrupts its lock, so
// the state holds a source the cache cannot read.
func damagedDeployment(t *testing.T, state, name string) string {
	t.Helper()
	root := filepath.Join(deploymentsPath(state), name)
	_, err := bundleWithMarker(t, name).Seed(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, lock.DefaultFilename), []byte("not: [yaml"), 0o600))
	return root
}

func TestSeedCacheReportsUnparsableRetainedModuleName(t *testing.T) {
	state, deployment, bundle := seededState(t)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("retained")))
	retainRow(t, deployment, lock.Module{Name: "wippy/agent/extra", Version: "0.1.0-dev", Hash: digest})

	_, err := seedCache(state, deployment, bundle)
	require.ErrorContains(t, err, "wippy/agent/extra")
}

func TestSeedCacheReportsMalformedRetainedModuleDigest(t *testing.T) {
	state, deployment, bundle := seededState(t)
	retainRow(t, deployment, lock.Module{Name: "wippy/agent", Version: "0.1.0-dev", Hash: "sha256:not-a-digest"})

	_, err := seedCache(state, deployment, bundle)
	require.ErrorContains(t, err, "wippy/agent")
}

// A lock row without a hash carries no immutable identity, so there is nothing
// to address it by in a content-addressed cache.
func TestSeedCacheAcceptsRetainedModuleWithoutDigest(t *testing.T) {
	state, deployment, bundle := seededState(t)
	retainRow(t, deployment, lock.Module{Name: "wippy/agent", Version: "0.1.0-dev"})

	_, err := seedCache(state, deployment, bundle)
	require.NoError(t, err)
}

func TestChangedBundleSeedsRetainedImmutableOverlayOffline(t *testing.T) {
	state := t.TempDir()
	oldBundle := bundleWithMarker(t, "old")
	oldDeployment := filepath.Join(deploymentsPath(state), oldBundle.ID())
	_, err := oldBundle.Seed(oldDeployment)
	require.NoError(t, err)
	oldLockBefore, err := os.ReadFile(filepath.Join(oldDeployment, lock.DefaultFilename))
	require.NoError(t, err)

	// An installed overlay is not necessarily listed in the shipped bundle's
	// lock. It is still a retained immutable artifact and is carried over.
	overlayRelative, overlayDigest := retainedOverlay(t, oldDeployment, "retained overlay artifact")

	newBundle := bundleWithMarker(t, "new")
	newDeployment := filepath.Join(deploymentsPath(state), newBundle.ID())
	_, err = newBundle.Seed(newDeployment)
	require.NoError(t, err)
	skipped, err := seedCache(state, newDeployment, newBundle)
	require.NoError(t, err)
	require.Empty(t, skipped)

	cache := cachePath(state)
	rootRelative := immutableRelative(t, "acme/app", "1.0.0", newBundle.Packs[0].Digest)
	require.NoError(t, hub.VerifyDownloadedArtifact(filepath.Join(cache, rootRelative), newBundle.Packs[0].Digest, uint64(len(newBundle.Packs[0].Data))))
	require.NoError(t, hub.VerifyDownloadedArtifact(filepath.Join(cache, overlayRelative), overlayDigest, 0))

	oldLock, err := os.ReadFile(filepath.Join(oldDeployment, lock.DefaultFilename))
	require.NoError(t, err)
	require.Equal(t, oldLockBefore, oldLock)
}

func TestSeedCacheRejectsSymlinkEscapeInTheSelectedDeployment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	state, deployment, bundle := seededState(t)
	outside := filepath.Join(t.TempDir(), "outside.wapp")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	symlink := filepath.Join(deployment, ".wippy", "vendor", "wippy",
		"agent-0.1.0-dev.sha256-"+fmt.Sprintf("%x", sha256.Sum256([]byte("outside")))+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(symlink), 0o700))
	require.NoError(t, os.Symlink(outside, symlink))

	_, err := seedCache(state, deployment, bundle)
	require.ErrorContains(t, err, "symlink")
}

func TestSeedCacheSkipsDamagedRetainedDeployment(t *testing.T) {
	state, deployment, bundle := seededState(t)
	damaged := damagedDeployment(t, state, "update-1")

	skipped, err := seedCache(state, deployment, bundle)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	require.ErrorContains(t, skipped[0], damaged)

	relative := immutableRelative(t, "acme/app", "1.0.0", bundle.Packs[0].Digest)
	require.NoError(t, hub.VerifyDownloadedArtifact(
		filepath.Join(cachePath(state), relative), bundle.Packs[0].Digest, uint64(len(bundle.Packs[0].Data))))
}

func TestSeedCacheFailsOnDamagedSelectedDeployment(t *testing.T) {
	state, deployment, bundle := seededState(t)
	require.NoError(t, os.WriteFile(filepath.Join(deployment, lock.DefaultFilename), []byte("not: [yaml"), 0o600))

	skipped, err := seedCache(state, deployment, bundle)
	require.Error(t, err)
	require.Empty(t, skipped)
}

func TestSeedCacheSkipsSymlinkInRetainedDeploymentVendor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	state, deployment, bundle := seededState(t)
	retained := filepath.Join(deploymentsPath(state), "update-1")
	_, err := bundleWithMarker(t, "symlink").Seed(retained)
	require.NoError(t, err)
	outside := filepath.Join(t.TempDir(), "outside.wapp")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	link := filepath.Join(retained, ".wippy", "vendor", "wippy",
		"agent-0.1.0-dev.sha256-"+fmt.Sprintf("%x", sha256.Sum256([]byte("outside")))+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.Symlink(outside, link))

	skipped, err := seedCache(state, deployment, bundle)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	require.ErrorContains(t, skipped[0], "symlink")
}

func TestSeedCacheImportsHealthyDeploymentsBesideADamagedOne(t *testing.T) {
	state, deployment, bundle := seededState(t)
	damagedDeployment(t, state, "update-1")
	healthy := filepath.Join(deploymentsPath(state), "update-2")
	_, err := bundleWithMarker(t, "healthy").Seed(healthy)
	require.NoError(t, err)
	relative, digest := retainedOverlay(t, healthy, "healthy overlay artifact")

	skipped, err := seedCache(state, deployment, bundle)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	require.NoError(t, hub.VerifyDownloadedArtifact(filepath.Join(cachePath(state), relative), digest, 0))
}

type offlineHub struct{ requests int }

func (h *offlineHub) GetManifest(context.Context, string, string, string) (*hub.ModuleManifest, error) {
	h.requests++
	return nil, fmt.Errorf("Hub must not be contacted during restore")
}

func (h *offlineHub) ListAllVersions(context.Context, string, string) ([]hub.VersionInfo, error) {
	h.requests++
	return nil, fmt.Errorf("Hub must not be contacted during restore")
}

func (h *offlineHub) GetDownloadURL(context.Context, *hub.DownloadParams) (*hub.DownloadInfo, error) {
	h.requests++
	return nil, fmt.Errorf("Hub must not be contacted during restore")
}

func (h *offlineHub) DownloadToFile(context.Context, string, string) error {
	h.requests++
	return fmt.Errorf("Hub must not be contacted during restore")
}

func TestChangedBundleRestoresHistoricalOverlayOffline(t *testing.T) {
	state := t.TempDir()
	oldBundle := bundleWithMarker(t, "old")
	oldDeployment := filepath.Join(deploymentsPath(state), oldBundle.ID())
	_, err := oldBundle.Seed(oldDeployment)
	require.NoError(t, err)
	newBundle := bundleWithMarker(t, "new")
	newDeployment := filepath.Join(deploymentsPath(state), newBundle.ID())
	newLock, err := newBundle.Seed(newDeployment)
	require.NoError(t, err)

	overlay := []byte("retained overlay artifact")
	overlayDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(overlay))
	_, _ = retainedOverlay(t, oldDeployment, string(overlay))
	skipped, err := seedCache(state, newDeployment, newBundle)
	require.NoError(t, err)
	require.Empty(t, skipped)

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

	client := &offlineHub{}
	handler, err := hub.NewDependencyHandler(hub.DependencyHandlerOptions{
		Hub: client, Logger: zap.NewNop(), LockPath: newLock, VendorDir: cachePath(state),
	})
	require.NoError(t, err)
	ctx := moduleapi.WithSourceRegistry(ctxapi.NewRootContext(), moduleapi.NewSourceRegistry())
	ctx = regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline)
	require.NoError(t, handler.PrepareRestore(ctx, history))
	require.Zero(t, client.requests)
	sources := moduleapi.GetSourceRegistry(ctx).Snapshot()
	require.Equal(t, newBundle.Packs[0].Digest, sources["acme/app"].Digest)
}
