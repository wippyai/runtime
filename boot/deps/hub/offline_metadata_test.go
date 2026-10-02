// SPDX-License-Identifier: MPL-2.0
package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestOfflineProviderPreservesCommittedArtifactMetadata(t *testing.T) {
	ctx := newTestContext()
	folder := t.TempDir()
	vendor := filepath.Join(folder, "vendor")
	artifact := buildWappBytes(t, []wapp.Entry{
		{ID: wapp.NewID("wippy.terminal", "definition"), Kind: regapi.NamespaceDefinition},
	})
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	require.NoError(t, os.MkdirAll(filepath.Join(vendor, "wippy"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendor, "wippy", "terminal-0.4.5.wapp"), artifact, 0o600))
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Logger: zap.NewNop(), Resolver: topology.NewResolver(),
		LockPath: filepath.Join(folder, "wippy.lock"), VendorDir: vendor,
	})
	require.NoError(t, err)
	// The shipped lock cannot encode the committed artifact size or protection.
	handler.lock.SetModule(lock.Module{Name: "wippy/terminal", Version: "0.4.5", Hash: digest})
	stored := &regapi.DependencyResolution{Modules: []regapi.ResolvedModule{{
		Name: "wippy/terminal", Version: "0.4.5", VersionID: "release-id", Source: "hub",
		Digest: digest, SizeBytes: uint64(len(artifact)), Protected: true,
	}}}
	provider := newLockedManifestProvider(handler, handler.offlineModules(stored))
	manifest, err := provider.GetManifest(ctx, "wippy", "terminal", "0.4.5")
	require.NoError(t, err)
	assert.Equal(t, "release-id", manifest.VersionID)
	assert.Equal(t, digest, manifest.Digest)
	assert.Equal(t, uint64(len(artifact)), manifest.SizeBytes)
	assert.True(t, manifest.Protected, "offline manifests must preserve protection from committed history")
}
