// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func TestColdRestartReconcilesRemovedReplacementBeforeLoadingArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		removed      bool
		historyOwned bool
	}{
		{name: "still-required"},
		{name: "removed-from-source", removed: true},
		{name: "history-owned-still-required", historyOwned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessUnspecified)
			f := newTestFixture(t)
			f.writeLock(t, "", "")
			digest, size, err := digestReplacementTree(f.replacementPath)
			require.NoError(t, err)
			_, recorded := f.newHistoryWithModule(t, moduleSourceReplacementTreeV1, digest, size)
			recorded.Deployment = nil
			oldBaseline := regapi.State{hardeningRoot("app:mod", "local/mod", "1.0.0")}
			oldBaseline[0].Registry.Root = !tc.historyOwned
			oldHandler := f.newHandler(t, f.replacements)
			recorded.BaselineDigest, err = oldHandler.deploymentBaselineDigest(ctx, oldBaseline, payload.GetTranscoder(ctx))
			require.NoError(t, err)
			recorded = *recorded.Canonical()

			dbPath := filepath.Join(f.root, "history.db")
			history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
			require.NoError(t, err)
			root, err := history.GetVersion(regapi.RootVersion)
			require.NoError(t, err)
			head := version.FromParent(root, 1)
			var authored regapi.ChangeSet
			if tc.historyOwned {
				authored = regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: oldBaseline[0]}}
			}
			require.NoError(t, history.SaveWithDependencyResolution(head, authored, &recorded, true))
			require.NoError(t, history.Close())

			// Cold start after removing workspace.replacements; only the positive case
			// also removes ns.dependency from the current source baseline.
			reopened, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
			require.NoError(t, err)
			defer reopened.Close()
			h := f.newHandler(t, nil)
			require.NoError(t, h.PrepareRestore(ctx, reopened))
			resolver := topology.NewResolver()
			h.resolver = resolver
			runner := &bootRecordingRunner{}
			reg := registryimpl.NewRegistry(reopened, runner, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
				registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(h.Expand).
					WithResolutionTransition(h.ReconcileResolution).WithChangesExpansion(h.ExpandChanges)))
			baseline := oldBaseline
			if tc.removed || tc.historyOwned {
				baseline = nil
			}
			err = reg.LoadState(regapi.WithRegistry(ctx, reg), baseline, head)
			if !tc.removed {
				require.Error(t, err)
				require.ErrorContains(t, err, "stored local replacement is not configured")
				require.Empty(t, runner.transitions, "no registry activation before a required source is verified")
				stored, getErr := reopened.GetDependencyResolution(head)
				require.NoError(t, getErr)
				require.Equal(t, recorded.Digest, stored.Digest, "failed boot must preserve its checkpoint")
				return
			}
			require.NoError(t, err)
			stored, err := reopened.GetDependencyResolution(head)
			require.NoError(t, err)
			require.Empty(t, stored.Modules)
			require.Empty(t, stored.Roots)
			// A second restart must work from the reconciled checkpoint too.
			require.NoError(t, f.newHandler(t, nil).PrepareRestore(ctx, reopened))
		})
	}
}
