// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"go.uber.org/zap"
)

// restoreHistoryWithResolution records one dependency resolution as history head.
func restoreHistoryWithResolution(t *testing.T, resolution *regapi.DependencyResolution) *memory.Storage {
	t.Helper()
	history := memory.New()
	root := version.New(0)
	require.NoError(t, history.SaveWithDependencyResolution(root, regapi.ChangeSet{}, nil, true))
	next := version.FromParent(root, 1)
	require.NoError(t, history.SaveWithDependencyResolution(next, regapi.ChangeSet{}, resolution, true))
	return history
}

func TestPrepareRestoreMaterializesEditedWorkspaceReplacement(t *testing.T) {
	ctx := newTestContext()
	rootDir := t.TempDir()
	lockPath := filepath.Join(rootDir, lock.DefaultFilename)
	replacementPath := filepath.Join(rootDir, "os-desktop")
	staticBundle := filepath.Join(replacementPath, "static", "app.js")

	require.NoError(t, os.MkdirAll(filepath.Dir(staticBundle), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
    modules: .wippy
    src: ./src
modules:
    - name: casha/os-desktop
      version: 0.0.0
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "_index.json"), []byte(`{
  "namespace": "casha.os_desktop",
  "entries": [{"name": "svc", "kind": "registry.entry", "data": {"generation": "one"}}]
}`), 0o600))
	require.NoError(t, os.WriteFile(staticBundle, []byte("window.build = 'one';\n"), 0o600))

	newHandler := func() *DependencyHandler {
		handler, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub:       &fakeHub{},
			Logger:    zap.NewNop(),
			LockPath:  lockPath,
			VendorDir: filepath.Join(rootDir, "vendor"),
			WorkspaceReplacements: []lock.Replacement{
				{From: "casha/os-desktop", To: replacementPath},
			},
		})
		require.NoError(t, err)
		return handler
	}

	recordedDigest, recordedSize, err := digestReplacementTree(replacementPath)
	require.NoError(t, err)
	resolution := dependencyResolution([]desiredDependency{{
		entry:      hardeningRoot("app.deps:desktop", "casha/os-desktop", "v0.0.0"),
		definition: DependencyDefinition{Component: "casha/os-desktop", Version: "v0.0.0"},
	}}, nil, []ResolvedModule{{
		Org: "casha", Name: "os-desktop", Version: "0.0.0",
		Source: moduleSourceReplacementTreeV1, Digest: recordedDigest, SizeBytes: recordedSize,
	}})
	history := restoreHistoryWithResolution(t, resolution)

	// An edit inside the replacement is the ordinary development case: the
	// recorded digest describes the run that wrote it, not a durable artifact.
	require.NoError(t, os.WriteFile(staticBundle, []byte("window.build = 'two';\n"), 0o600))
	editedDigest, editedSize, err := digestReplacementTree(replacementPath)
	require.NoError(t, err)
	require.NotEqual(t, recordedDigest, editedDigest)

	handler := newHandler()
	require.NoError(t, handler.PrepareRestore(ctx, history))

	modulePath, err := handler.ensureModuleAvailable(ctx, ResolvedModule{
		Org: "casha", Name: "os-desktop", Version: "0.0.0",
		Source: moduleSourceReplacementTreeV1, Digest: editedDigest, SizeBytes: editedSize,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(modulePath, "static", "app.js"))
	require.NoError(t, err)
	require.Equal(t, "window.build = 'two';\n", string(content))
}

func TestPrepareRestorePublishesEditedReplacementIdentity(t *testing.T) {
	ctx := newTestContext()
	rootDir := t.TempDir()
	lockPath := filepath.Join(rootDir, lock.DefaultFilename)
	replacementPath := filepath.Join(rootDir, "os-desktop")

	require.NoError(t, os.MkdirAll(replacementPath, 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
    modules: .wippy
    src: ./src
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "_index.json"), []byte(`{
  "namespace": "casha.os_desktop",
  "entries": [{"name": "svc", "kind": "registry.entry", "data": {"generation": "one"}}]
}`), 0o600))

	recordedDigest, recordedSize, err := digestReplacementTree(replacementPath)
	require.NoError(t, err)
	module := regapi.ResolvedModule{
		Name: "casha/os-desktop", Version: "0.0.0", VersionID: "0.0.0",
		Source: moduleSourceReplacementTreeV1, Digest: recordedDigest, SizeBytes: recordedSize,
	}
	resolution := dependencyResolution([]desiredDependency{{
		entry:      hardeningRoot("app.deps:desktop", "casha/os-desktop", "v0.0.0"),
		definition: DependencyDefinition{Component: "casha/os-desktop", Version: "v0.0.0"},
	}}, nil, []ResolvedModule{{
		Org: "casha", Name: "os-desktop", Version: "0.0.0",
		Source: moduleSourceReplacementTreeV1, Digest: recordedDigest, SizeBytes: recordedSize,
	}})
	resolution.Deployment = (&regapi.Deployment{
		Root:    "casha/os-desktop",
		Modules: []regapi.ResolvedModule{module},
	}).Canonical()
	resolution = resolution.Canonical()
	history := restoreHistoryWithResolution(t, resolution)

	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "_index.json"), []byte(`{
  "namespace": "casha.os_desktop",
  "entries": [{"name": "svc", "kind": "registry.entry", "data": {"generation": "two"}}]
}`), 0o600))
	editedDigest, _, err := digestReplacementTree(replacementPath)
	require.NoError(t, err)
	require.NotEqual(t, recordedDigest, editedDigest)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: filepath.Join(rootDir, "vendor"),
		WorkspaceReplacements: []lock.Replacement{
			{From: "casha/os-desktop", To: replacementPath},
		},
	})
	require.NoError(t, err)

	sources := moduleapi.NewSourceRegistry()
	ctx = moduleapi.WithSourceRegistry(ctx, sources)
	require.NoError(t, handler.PrepareRestore(ctx, history))

	source, ok := sources.Snapshot()["casha/os-desktop"]
	require.True(t, ok, "restore publishes the replacement source")
	require.True(t, source.Replacement)
	require.Equal(t, editedDigest, source.Digest,
		"restore republishes the replacement at its current tree identity")
}

func TestPrepareRestoreRejectsAlteredHubArtifact(t *testing.T) {
	ctx := newTestContext()
	rootDir := t.TempDir()
	lockPath := filepath.Join(rootDir, lock.DefaultFilename)
	vendorDir := filepath.Join(rootDir, "vendor")

	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
    modules: .wippy
    src: ./src
`), 0o600))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
		WorkspaceReplacements: []lock.Replacement{
			{From: "casha/os-desktop", To: filepath.Join(rootDir, "os-desktop")},
		},
	})
	require.NoError(t, err)

	name, err := graph.ParseName("wippy/llm")
	require.NoError(t, err)
	published := []byte("published artifact")
	sum := sha256.Sum256(published)
	recordedDigest := "sha256:" + hex.EncodeToString(sum[:])
	artifactPath, err := handler.immutableArtifactPath(name, "0.4.46", recordedDigest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(artifactPath), 0o755))
	require.NoError(t, os.WriteFile(artifactPath, []byte("tampered artifact"), 0o600))

	resolution := dependencyResolution([]desiredDependency{{
		entry:      hardeningRoot("app.deps:llm", "wippy/llm", "v0.4.46"),
		definition: DependencyDefinition{Component: "wippy/llm", Version: "v0.4.46"},
	}}, nil, []ResolvedModule{{
		Org: "wippy", Name: "llm", Version: "0.4.46",
		Source: moduleSourceHub, Digest: recordedDigest, SizeBytes: uint64(len(published)),
	}})
	history := restoreHistoryWithResolution(t, resolution)

	err = handler.PrepareRestore(ctx, history)
	require.Error(t, err, "a hub artifact must still be verified against its recorded digest")
	require.Contains(t, err.Error(), "wippy/llm")
}
