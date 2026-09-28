// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

// alteredArtifactRestore builds a handler whose history records one Hub module
// at an exact version and digest, and counts every Hub interaction.
type alteredArtifactRestore struct {
	handler      *DependencyHandler
	history      regapi.History
	vendorDir    string
	name         graph.Name
	digest       string
	published    []byte
	downloadURLs int
	downloads    int
}

func newAlteredArtifactRestore(t *testing.T) *alteredArtifactRestore {
	t.Helper()
	rootDir := t.TempDir()
	lockPath := filepath.Join(rootDir, lock.DefaultFilename)
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
    modules: .wippy
    src: ./src
`), 0o600))

	published := []byte("published artifact")
	sum := sha256.Sum256(published)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	fixture := &alteredArtifactRestore{
		vendorDir: filepath.Join(rootDir, "vendor"),
		digest:    digest,
		published: published,
	}
	client := &fakeHub{
		getDownload: func(_ context.Context, params *DownloadParams) (*DownloadInfo, error) {
			fixture.downloadURLs++
			require.Equal(t, "wippy", params.Org)
			require.Equal(t, "llm", params.Module)
			return &DownloadInfo{URL: "memory://llm", Digest: digest, Size: uint64(len(published))}, nil
		},
		downloadFile: func(_ context.Context, _ string, destination string) error {
			fixture.downloads++
			return os.WriteFile(destination, published, 0o600)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       client,
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: fixture.vendorDir,
	})
	require.NoError(t, err)
	fixture.handler = handler

	name, err := graph.ParseName("wippy/llm")
	require.NoError(t, err)
	fixture.name = name

	resolution := dependencyResolution([]desiredDependency{{
		entry:      hardeningRoot("app.deps:llm", "wippy/llm", "v0.4.46"),
		definition: DependencyDefinition{Component: "wippy/llm", Version: "v0.4.46"},
	}}, nil, []ResolvedModule{{
		Org: "wippy", Name: "llm", Version: "0.4.46",
		Source: moduleSourceHub, Digest: digest, SizeBytes: uint64(len(published)),
	}})
	fixture.history = restoreHistoryWithResolution(t, resolution)
	return fixture
}

// writeAltered stores content that does not match the recorded digest at the
// given cache path.
func (f *alteredArtifactRestore) writeAltered(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("tampered artifact"), 0o600))
}

func (f *alteredArtifactRestore) immutablePath(t *testing.T) string {
	t.Helper()
	path, err := f.handler.immutableArtifactPath(f.name, "0.4.46", f.digest)
	require.NoError(t, err)
	return path
}

func (f *alteredArtifactRestore) legacyPath() string {
	return filepath.Join(f.vendorDir, lock.WappPath(f.name, "0.4.46"))
}

func TestPrepareRestoreOfflineRefusesAlteredArtifactWithoutHub(t *testing.T) {
	t.Run("digest addressed cache entry", func(t *testing.T) {
		fixture := newAlteredArtifactRestore(t)
		fixture.writeAltered(t, fixture.immutablePath(t))

		offline := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
		err := fixture.handler.PrepareRestore(offline, fixture.history)
		require.Error(t, err, "an altered cache entry must refuse an offline restore")

		var apiErr apierror.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, "wippy/llm@0.4.46", apiErr.Details().GetString("module", ""))
		require.Contains(t, err.Error(), "wippy/llm")
		require.Zero(t, fixture.downloadURLs, "an offline restore must not ask the Hub where to download")
		require.Zero(t, fixture.downloads, "an offline restore must never replace a local artifact from the Hub")
	})

	t.Run("version addressed cache entry", func(t *testing.T) {
		fixture := newAlteredArtifactRestore(t)
		fixture.writeAltered(t, fixture.legacyPath())

		offline := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
		err := fixture.handler.PrepareRestore(offline, fixture.history)
		require.Error(t, err, "an altered cache entry must refuse an offline restore")

		var apiErr apierror.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, "wippy/llm@0.4.46", apiErr.Details().GetString("module", ""))
		require.Contains(t, err.Error(), "wippy/llm")
		require.Contains(t, apiErr.Details().GetString("hint", ""), "wippy update/install")
		require.Zero(t, fixture.downloadURLs, "an offline restore must not ask the Hub where to download")
		require.Zero(t, fixture.downloads, "an offline restore must never replace a local artifact from the Hub")
	})
}

// TestPrepareRestoreOnlineAlteredArtifactContrast records what the same altered
// cache entries do once the caller grants online access.
func TestPrepareRestoreOnlineAlteredArtifactContrast(t *testing.T) {
	t.Run("digest addressed cache entry stays refused", func(t *testing.T) {
		fixture := newAlteredArtifactRestore(t)
		artifactPath := fixture.immutablePath(t)
		fixture.writeAltered(t, artifactPath)

		online := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessOnline)
		err := fixture.handler.PrepareRestore(online, fixture.history)
		require.Error(t, err, "a digest addressed cache entry is verified before any download is considered")

		var apiErr apierror.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, "wippy/llm@0.4.46", apiErr.Details().GetString("module", ""))
		require.Zero(t, fixture.downloadURLs)
		require.Zero(t, fixture.downloads)

		stored, err := os.ReadFile(artifactPath)
		require.NoError(t, err)
		require.Equal(t, []byte("tampered artifact"), stored, "a refused restore must leave the cache untouched")
	})

	t.Run("version addressed cache entry is refetched", func(t *testing.T) {
		fixture := newAlteredArtifactRestore(t)
		legacyPath := fixture.legacyPath()
		fixture.writeAltered(t, legacyPath)

		online := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessOnline)
		require.NoError(t, fixture.handler.PrepareRestore(online, fixture.history))
		require.Equal(t, 1, fixture.downloadURLs)
		require.Equal(t, 1, fixture.downloads)

		stored, err := os.ReadFile(fixture.immutablePath(t))
		require.NoError(t, err)
		require.Equal(t, fixture.published, stored)

		legacy, err := os.ReadFile(legacyPath)
		require.NoError(t, err)
		require.Equal(t, []byte("tampered artifact"), legacy, "migration input is never rewritten in place")
	})
}
