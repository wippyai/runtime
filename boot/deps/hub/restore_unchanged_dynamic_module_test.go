// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	expansion "github.com/wippyai/runtime/system/registry/expansion"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// A module installed at runtime is materialized by dependency expansion; its
// entries are not part of any authored changeset. Restoring a historical
// version whose resolution selects the same artifact keeps those resident
// entries instead of deleting them.
func TestApplyVersionKeepsUnchangedRuntimeInstalledModuleEntries(t *testing.T) {
	ctx := newTestContext()
	directory := t.TempDir()
	vendor := filepath.Join(directory, "vendor")
	lockPath := filepath.Join(directory, "wippy.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte("directories:\n  modules: vendor\nmodules: []\n"), 0o600))

	artifact := buildWappBytes(t, []wapp.Entry{
		{ID: wapp.NewID("acme.feature", "sentinel"), Kind: regapi.EntryKind, Data: map[string]any{"value": "installed"}},
		{ID: wapp.NewID("acme.feature", "definition"), Kind: regapi.NamespaceDefinition, Data: map[string]any{"source": "return 1"}},
	})
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(artifact))
	client := &fakeHub{
		getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
			if org+"/"+module != "acme/feature" {
				return nil, fmt.Errorf("unexpected manifest %s/%s@%s", org, module, constraint)
			}
			return &ModuleManifest{Org: org, Name: module, Version: "1.0.0", VersionID: "1.0.0",
				Digest: digest, SizeBytes: uint64(len(artifact)), URL: "memory://feature@1.0.0"}, nil
		},
		listVersions: func(_ context.Context, _, _ string) ([]VersionInfo, error) {
			return []VersionInfo{{Version: "1.0.0"}}, nil
		},
		downloadFile: func(_ context.Context, url, destination string) error {
			if url != "memory://feature@1.0.0" {
				return fmt.Errorf("unexpected download %s", url)
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return err
			}
			return os.WriteFile(destination, artifact, 0o600)
		},
	}

	history, err := historysqlite.NewSQLite(filepath.Join(directory, "history.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })
	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(),
		Resolver: resolver, LockPath: lockPath, VendorDir: vendor})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	reg := registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver),
		resolver, zap.NewNop(), registryimpl.WithKindDirective(regapi.NamespaceDependency,
			expansion.NewDependencyDirective(handler.Expand).WithChangesExpansion(handler.ExpandChanges).
				WithResolutionTransition(handler.ReconcileResolution)))
	ctx = regapi.WithRegistry(ctx, reg)

	app := regapi.Entry{ID: regapi.NewID("app", "config"), Kind: regapi.EntryKind, Data: payload.New(map[string]any{"ok": true})}
	base := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, regapi.State{app}, base))

	root := regapi.Entry{ID: regapi.NewID("app.deps", "feature"), Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{"description": "installed"}),
		Data: payload.New(map[string]any{"component": "acme/feature", "version": "*"})}
	installed, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: root}})
	require.NoError(t, err)
	before := sortedEntryIDs(reg.Snapshot().Entries)
	require.Contains(t, before, "acme.feature:sentinel", "installation materializes the module entries")

	candidate := root
	candidate.Meta = attrs.NewBagFrom(map[string]any{"description": "candidate configuration"})
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: candidate}})
	require.NoError(t, err)

	require.NoError(t, reg.ApplyVersion(ctx, installed))
	require.Equal(t, before, sortedEntryIDs(reg.Snapshot().Entries),
		"restoring a version with the same selected artifact keeps the resident module entries")
	sentinel, err := reg.GetEntry(regapi.NewID("acme.feature", "sentinel"))
	require.NoError(t, err)
	require.Equal(t, "acme/feature", sentinel.Registry.Owner)

	require.NoError(t, reg.ApplyVersion(ctx, base))
	require.Equal(t, []string{"app:config"}, sortedEntryIDs(reg.Snapshot().Entries),
		"restoring a version before the installation removes the module entries")
}

func sortedEntryIDs(entries regapi.State) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID.String())
	}
	sort.Strings(ids)
	return ids
}
