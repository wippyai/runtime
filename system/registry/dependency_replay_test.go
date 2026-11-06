// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestDependencyReplayCapturesCompactDeletesAndKindChanges(t *testing.T) {
	dependency := regapi.Entry{
		ID: regapi.NewID("app.deps", "worker"), Kind: regapi.NamespaceDependency,
		Registry: regapi.EntryMetadata{Owner: "acme/app", Root: true},
	}
	compact := regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: dependency.ID}}
	changes := regapi.ChangeSet{compact}
	projection := regapi.NewReplayState()
	require.NoError(t, projection.ApplyWithTranscoder(changes, nil))
	captured, ok := projection.LastDependencyOperation(dependency.ID, regapi.State{dependency})
	require.True(t, ok)
	require.Equal(t, dependency, captured.Entry)
	require.Equal(t, compact, changes[0], "replay classification must not rewrite history")

	retagged := dependency
	retagged.Kind = regapi.EntryKind
	changes = regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: retagged, OriginalEntry: &dependency}}
	projection = regapi.NewReplayState()
	require.NoError(t, projection.ApplyWithTranscoder(changes, nil))
	captured, ok = projection.LastDependencyOperation(dependency.ID, regapi.State{dependency})
	require.True(t, ok)
	require.Equal(t, changes[0], captured.Operation, "replacing a dependency with ordinary data must survive artifact restore")
	changes = regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "data"), Kind: regapi.EntryKind}}}
	projection = regapi.NewReplayState()
	require.NoError(t, projection.ApplyWithTranscoder(changes, nil))
	_, ok = projection.LastDependencyOperation(changes[0].Entry.ID, regapi.State{dependency})
	require.False(t, ok)
}
