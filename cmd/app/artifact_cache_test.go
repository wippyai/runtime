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
// executables select distinct embedded deployments in the same state.
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

// retainRow adds a module row to a retained deployment lock without touching the
// artifacts it points at, reproducing a lock a previous runtime left behind.
func retainRow(t *testing.T, deployment string, module lock.Module) {
	t.Helper()
	locked, err := lock.New(filepath.Join(deployment, lock.DefaultFilename))
	require.NoError(t, err)
	locked.SetModule(module)
	require.NoError(t, locked.Write())
}

func seededDeployment(t *testing.T) (string, string, Bundle) {
	t.Helper()
	state := t.TempDir()
	bundle := bundleWithMarker(t, "stable")
	deployment := embeddedDeployment(state, bundle)
	_, err := bundle.Seed(deployment)
	require.NoError(t, err)
	return state, deployment, bundle
}

func TestSeedDependencyCacheReportsUnparsableRetainedModuleName(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("retained")))
	retainRow(t, deployment, lock.Module{Name: "wippy/agent/extra", Version: "0.1.0-dev", Hash: digest})

	_, err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.ErrorContains(t, err, "wippy/agent/extra")
}

func TestSeedDependencyCacheReportsMalformedRetainedModuleDigest(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	retainRow(t, deployment, lock.Module{Name: "wippy/agent", Version: "0.1.0-dev", Hash: "sha256:not-a-digest"})

	_, err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.ErrorContains(t, err, "wippy/agent")
}

// A lock row without a hash carries no immutable identity, so there is nothing
// to address it by in a content-addressed cache. It is not corruption.
func TestSeedDependencyCacheAcceptsRetainedModuleWithoutDigest(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	retainRow(t, deployment, lock.Module{Name: "wippy/agent", Version: "0.1.0-dev"})

	_, seedErr := seedDependencyCache(state, deployment, bundle)
	require.NoError(t, seedErr)
}

func TestSeedDependencyCacheReportsCorruptActivation(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	require.NoError(t, os.WriteFile(filepath.Join(state, "active.json"), []byte("{"), 0o600))

	_, err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.ErrorContains(t, err, "activation")
}

// immutableRelative names a cache entry through the same contract production
// code publishes it under.
func immutableRelative(t *testing.T, module, version, digest string) string {
	t.Helper()
	_, relative, err := immutableArtifactPath(module, version, digest)
	require.NoError(t, err)
	return relative
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
	overlayRelative := immutableRelative(t, "wippy/agent", "0.1.0-dev", overlayDigest)
	overlayPath := filepath.Join(oldDeployment, ".wippy", "vendor", overlayRelative)
	require.NoError(t, os.MkdirAll(filepath.Dir(overlayPath), 0o700))
	require.NoError(t, os.WriteFile(overlayPath, overlay, 0o600))

	newBundle := bundleWithMarker(t, "new")
	newDeployment := embeddedDeployment(state, newBundle)
	_, err = newBundle.Seed(newDeployment)
	require.NoError(t, err)
	_, seedErr := seedDependencyCache(state, newDeployment, newBundle)
	require.NoError(t, seedErr)

	cache := dependencyVendorDirectory(state)
	rootRelative := immutableRelative(t, "acme/app", "1.0.0", newBundle.Packs[0].Digest)
	require.NoError(t, hub.VerifyDownloadedArtifact(filepath.Join(cache, rootRelative), newBundle.Packs[0].Digest, uint64(len(newBundle.Packs[0].Data))))
	require.NoError(t, hub.VerifyDownloadedArtifact(filepath.Join(cache, overlayRelative), overlayDigest, uint64(len(overlay))))

	oldLock, err := os.ReadFile(filepath.Join(oldDeployment, lock.DefaultFilename))
	require.NoError(t, err)
	require.Equal(t, oldLockBefore, oldLock)
}

func TestDependencyCacheRejectsRetainedSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	state, deployment, bundle := seededDeployment(t)
	outside := filepath.Join(t.TempDir(), "outside.wapp")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	symlink := filepath.Join(deployment, ".wippy", "vendor", "wippy", "agent-0.1.0-dev.sha256-"+fmt.Sprintf("%x", sha256.Sum256([]byte("outside")))+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(symlink), 0o700))
	require.NoError(t, os.Symlink(outside, symlink))
	_, seedErr := seedDependencyCache(state, deployment, bundle)
	require.ErrorContains(t, seedErr, "symlink")
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
	overlayRelative := immutableRelative(t, "wippy/agent", "0.1.0-dev", overlayDigest)
	overlayPath := filepath.Join(oldDeployment, ".wippy", "vendor", overlayRelative)
	require.NoError(t, os.MkdirAll(filepath.Dir(overlayPath), 0o700))
	require.NoError(t, os.WriteFile(overlayPath, overlay, 0o600))
	_, seedErr := seedDependencyCache(state, newDeployment, newBundle)
	require.NoError(t, seedErr)

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

// damagedRevision seeds a retained revision and then corrupts its lock, so the
// revision exists in the state directory as a source the cache cannot read.
func damagedRevision(t *testing.T, state, name string) string {
	t.Helper()
	root := filepath.Join(state, "revisions", name, "deployment")
	_, err := bundleWithMarker(t, name).Seed(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, lock.DefaultFilename), []byte("not: [yaml"), 0o600))
	return root
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

func TestSeedDependencyCacheSkipsDamagedRetainedRevision(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	damaged := damagedRevision(t, state, "revision-damaged")

	skipped, err := seedDependencyCache(state, deployment, bundle)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	require.ErrorContains(t, skipped[0], damaged)

	relative := immutableRelative(t, "acme/app", "1.0.0", bundle.Packs[0].Digest)
	require.NoError(t, hub.VerifyDownloadedArtifact(
		filepath.Join(dependencyVendorDirectory(state), relative),
		bundle.Packs[0].Digest, uint64(len(bundle.Packs[0].Data))))
}

func TestSeedDependencyCacheFailsOnDamagedSelectedDeployment(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	require.NoError(t, os.WriteFile(filepath.Join(deployment, lock.DefaultFilename), []byte("not: [yaml"), 0o600))

	skipped, err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.Empty(t, skipped)
}

func TestSeedDependencyCacheSkipsSymlinkInRetainedRevisionVendor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	state, deployment, bundle := seededDeployment(t)
	revision := filepath.Join(state, "revisions", "revision-symlink", "deployment")
	_, err := bundleWithMarker(t, "symlink").Seed(revision)
	require.NoError(t, err)
	outside := filepath.Join(t.TempDir(), "outside.wapp")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	link := filepath.Join(revision, ".wippy", "vendor", "wippy",
		"agent-0.1.0-dev.sha256-"+fmt.Sprintf("%x", sha256.Sum256([]byte("outside")))+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.Symlink(outside, link))

	skipped, err := seedDependencyCache(state, deployment, bundle)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	require.ErrorContains(t, skipped[0], "symlink")
}

func TestSeedDependencyCacheImportsHealthyRootsBesideDamagedRoot(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	damagedRevision(t, state, "revision-damaged")
	healthy := filepath.Join(state, "revisions", "revision-healthy", "deployment")
	_, err := bundleWithMarker(t, "healthy").Seed(healthy)
	require.NoError(t, err)
	relative, digest := retainedOverlay(t, healthy, "healthy overlay artifact")

	skipped, err := seedDependencyCache(state, deployment, bundle)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	require.NoError(t, hub.VerifyDownloadedArtifact(
		filepath.Join(dependencyVendorDirectory(state), relative), digest, 0))
}
