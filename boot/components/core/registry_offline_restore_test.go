// SPDX-License-Identifier: MPL-2.0

package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/internal/version"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"go.uber.org/zap"
)

// TestRegistryComponentRestoresDependenciesOffline pins the dependency access
// policy the registry component hands to startup restore. The recorded module
// has no local artifact, so an offline restore refuses it by evidence while an
// online restore would reach the Hub for a download instead.
func TestRegistryComponentRestoresDependenciesOffline(t *testing.T) {
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "registry.db")
	lockPath := filepath.Join(rootDir, "wippy.lock")
	vendorDir := filepath.Join(rootDir, "vendor")
	require.NoError(t, os.MkdirAll(vendorDir, 0o755))

	sum := sha256.Sum256([]byte("published artifact"))
	hexDigest := hex.EncodeToString(sum[:])

	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
    modules: vendor
modules:
    - name: wippy/llm
      version: 0.4.46
      hash: sha256:`+hexDigest+`
      root: true
`), 0o600))

	seedDependencyResolution(t, dbPath, "wippy/llm", "0.4.46", "sha256:"+hexDigest)

	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryEnableHistory:       true,
		RegistryHistoryType:         "sqlite",
		RegistryHistoryPath:         dbPath,
		RegistryDependencyLockPath:  lockPath,
		RegistryDependencyVendorDir: vendorDir,
	}))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)

	loader, err := bootpkg.NewLoader(Artifacts(), Registry())
	require.NoError(t, err)

	_, err = loader.Load(ctx)
	require.Error(t, err, "startup must refuse a recorded module that has no local artifact")

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)

	offline := offlineDependencyError(t, err)
	require.Contains(t, offline.Error(), "offline startup")
	require.Contains(t, offline.Details().GetString("hint", ""), "wippy update/install")
	require.Equal(t, "wippy/llm@0.4.46", offline.Details().GetString("module", ""))

	// A failed start releases the history it opened: sqlite drops the WAL
	// sidecars only when the last connection closes cleanly.
	require.NoFileExists(t, dbPath+"-wal", "history left open after a failed start")
	require.NoFileExists(t, dbPath+"-shm", "history left open after a failed start")
	entries, readErr := os.ReadDir(vendorDir)
	require.NoError(t, readErr)
	require.Empty(t, entries, "a refused startup must not materialize any artifact")
}

// seedDependencyResolution records one dependency resolution as the head of a
// sqlite registry history the component later opens by path.
func seedDependencyResolution(t *testing.T, dbPath, module, moduleVersion, digest string) {
	t.Helper()
	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)

	root, err := history.GetVersion(regapi.RootVersion)
	require.NoError(t, err)
	head := version.FromParent(root, 1)
	resolved := regapi.ResolvedModule{
		Name: module, Version: moduleVersion, VersionID: moduleVersion, Source: "hub", Digest: digest,
	}
	resolution := (&regapi.DependencyResolution{
		Roots:      []regapi.DependencyRoot{{ID: "app.deps:llm", Component: module, Version: moduleVersion}},
		Modules:    []regapi.ResolvedModule{resolved},
		Deployment: &regapi.Deployment{Root: module, Modules: []regapi.ResolvedModule{resolved}},
	}).Canonical()
	require.NoError(t, history.SaveWithDependencyResolution(head, nil, resolution, true))
	require.NoError(t, history.Close())
}

// offlineDependencyError returns the categorized offline refusal carried by the
// startup error chain. Component loading wraps its cause in its own detail-free
// categorized error, so the chain is walked rather than matched at the top.
func offlineDependencyError(t *testing.T, err error) apierror.Error {
	t.Helper()
	for current := err; current != nil; current = errors.Unwrap(current) {
		var candidate apierror.Error
		if !errors.As(current, &candidate) {
			continue
		}
		if candidate.Details() != nil && candidate.Details().GetString("hint", "") != "" {
			return candidate
		}
	}
	require.FailNow(t, "startup error chain carries no offline dependency refusal", "%v", err)
	return nil
}
