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
	state := regapi.StateMap{dependency.ID: dependency}
	compact := regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: dependency.ID}}
	changes := regapi.ChangeSet{compact}
	captured := appendDependencyReplayChanges(nil, changes, state)
	require.Len(t, captured, 1)
	require.Equal(t, dependency, captured[0][0].Entry)
	require.Equal(t, compact, changes[0], "replay classification must not rewrite history")

	retagged := dependency
	retagged.Kind = regapi.EntryKind
	changes = regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: retagged, OriginalEntry: &dependency}}
	captured = appendDependencyReplayChanges(nil, changes, state)
	require.Equal(t, []regapi.ChangeSet{changes}, captured, "replacing a dependency with ordinary data must survive artifact restore")
	changes = regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "data"), Kind: regapi.EntryKind}}}
	require.Empty(t, appendDependencyReplayChanges(nil, changes, state))
}
