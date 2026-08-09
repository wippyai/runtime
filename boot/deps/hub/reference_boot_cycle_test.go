// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
)

// The full referenced-resolution lifecycle through the real registry, planner,
// and sqlite history: fold on Apply, strict replay across close/reopen,
// promotion on controller delete, and replay again after the promotion.
func TestReferencedResolutionFullBootCycle(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	vendorDir := filepath.Join(tmpDir, "vendor")
	dbPath := filepath.Join(tmpDir, "registry.db")

	artifact := buildWappBytes(t, []wapp.Entry{{
		ID: wapp.NewID("acme.app", "service"), Kind: "service", Data: map[string]any{"version": "v1"},
	}})
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/app
    version: v1.0.0
    hash: %s
    root: true
`, digest)), 0o600))

	hub := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			if org != "acme" || module != "app" {
				return nil, fmt.Errorf("unexpected module %s/%s", org, module)
			}
			return &ModuleManifest{
				Org: org, Name: module, Version: "v1.0.0", VersionID: "v1.0.0",
				Digest: digest, SizeBytes: uint64(len(artifact)), URL: "memory://app",
			}, nil
		},
		downloadFile: func(_ context.Context, _ string, destination string) error {
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destination, artifact, 0o600)
		},
	}

	root := regapi.Entry{
		ID: regapi.NewID("app.deps", "app"), Kind: regapi.NamespaceDependency,
		DependencyRoot: true,
		Data:           payload.New(map[string]any{"component": "acme/app", "version": "v1.0.0"}),
	}
	service := markModuleIdentity(regapi.Entry{
		ID: regapi.NewID("acme.app", "service"), Kind: "service",
		Data: payload.New(map[string]any{"version": "1"}),
	}, "acme/app", "v1.0.0", digest)
	baseline := regapi.State{root, service}
	reference := regapi.Entry{
		ID:   regapi.NewID("acme.pkg", "__dependency.acme.app"),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/app", "version": ">=1.0.0"}),
	}

	newRegistry := func(history regapi.History) *registryimpl.Reg {
		resolver := topology.NewResolver()
		handler, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub: hub, Logger: zap.NewNop(), Resolver: resolver, LockPath: lockPath, VendorDir: vendorDir,
		})
		require.NoError(t, err)
		return registryimpl.NewRegistry(
			history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
			registryimpl.WithKindDirective(regapi.NamespaceDependency,
				expansion.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution).WithChangesExpansion(handler.ExpandChanges)),
		)
	}

	// Cycle 1: fold on Apply and checkpoint the referenced graph.
	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	reg := newRegistry(history)
	require.NoError(t, reg.LoadState(ctx, baseline, version.FromParent(nil, 0)))
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: reference}})
	require.NoError(t, err)
	head, err := history.Head()
	require.NoError(t, err)
	recorded, err := history.GetDependencyResolution(head)
	require.NoError(t, err)
	require.Len(t, recorded.Roots, 1)
	require.Equal(t, "app.deps:app", recorded.Roots[0].ID)
	require.Len(t, recorded.References, 1)
	require.Equal(t, "acme.pkg:__dependency.acme.app", recorded.References[0].ID)
	require.NoError(t, history.Close())

	// Cycle 2: replay the referenced version across a process restart.
	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	reg = newRegistry(history)
	require.NoError(t, reg.LoadState(ctx, baseline, head))
	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	found := false
	for _, entry := range entries {
		if entry.ID.String() == "acme.pkg:__dependency.acme.app" {
			found = true
		}
	}
	require.True(t, found, "replayed state must carry the reference declaration")

	// Cycle 3: deleting the controller promotes the reference in the recorded
	// graph and keeps the module installed.
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: root.ID}}})
	require.NoError(t, err)
	head, err = history.Head()
	require.NoError(t, err)
	promoted, err := history.GetDependencyResolution(head)
	require.NoError(t, err)
	require.Len(t, promoted.Roots, 1)
	require.Equal(t, "acme.pkg:__dependency.acme.app", promoted.Roots[0].ID)
	require.Empty(t, promoted.References)
	entries, err = reg.GetAllEntries()
	require.NoError(t, err)
	moduleAlive := false
	for _, entry := range entries {
		if entry.ID.String() == "acme.app:service" {
			moduleAlive = true
		}
	}
	require.True(t, moduleAlive, "promotion must not uninstall the module")
	require.NoError(t, history.Close())

	// Cycle 4: the promoted graph replays across another restart.
	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = history.Close() }()
	reg = newRegistry(history)
	require.NoError(t, reg.LoadState(ctx, baseline, head))

	// Cycle 5: undo to the referenced version and redo to the promoted one.
	// Each transition replays its own stored graph within the unchanged
	// baseline; neither graph is rebound and both stay content-distinct.
	promotedVersion := head
	referencedVersion := head.Previous()
	require.NoError(t, reg.ApplyVersion(ctx, referencedVersion))
	undone, err := history.GetDependencyResolution(referencedVersion)
	require.NoError(t, err)
	require.Equal(t, recorded.Digest, undone.Digest, "undo must not rebind the referenced graph")
	entries, err = reg.GetAllEntries()
	require.NoError(t, err)
	byID := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		byID[entry.ID.String()] = struct{}{}
	}
	require.Contains(t, byID, "app.deps:app", "undo must restore the controller declaration")
	require.Contains(t, byID, "acme.pkg:__dependency.acme.app")
	require.Contains(t, byID, "acme.app:service")

	require.NoError(t, reg.ApplyVersion(ctx, promotedVersion))
	redone, err := history.GetDependencyResolution(promotedVersion)
	require.NoError(t, err)
	require.Equal(t, promoted.Digest, redone.Digest, "redo must not rebind the promoted graph")
	require.NotEqual(t, undone.Digest, redone.Digest)
	entries, err = reg.GetAllEntries()
	require.NoError(t, err)
	byID = make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		byID[entry.ID.String()] = struct{}{}
	}
	require.NotContains(t, byID, "app.deps:app", "redo must drop the deleted controller again")
	require.Contains(t, byID, "acme.app:service", "the module must survive both transitions")
}
