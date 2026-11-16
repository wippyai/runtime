// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
)

type replayStateTestTranscoder struct{}

func (replayStateTestTranscoder) Transcode(value payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(value.Data(), format), nil
}

func (replayStateTestTranscoder) Unmarshal(value payload.Payload, target any) error {
	declaration, ok := target.(*DependencyDeclaration)
	if !ok {
		return fmt.Errorf("unexpected target %T", target)
	}
	data, ok := value.Data().(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected dependency data %T", value.Data())
	}
	declaration.Component, _ = data["component"].(string)
	return nil
}

func replayStateTestContext() context.Context {
	return payload.WithTranscoder(ctxapi.NewRootContext(), replayStateTestTranscoder{})
}

func TestReplayStateCompactsDependencyReplayEvidence(t *testing.T) {
	state := NewReplayState()
	owner := Entry{ID: NewID("deps", "app"), Kind: NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/app", "version": "1"})}
	previous := Entry{ID: NewID("app", "worker"), Kind: NamespaceDependency,
		Registry: EntryMetadata{Owner: "acme/app"}, Data: payload.New(map[string]any{"component": "acme/worker", "version": "1"})}
	changed := previous
	changed.Data = payload.New(map[string]any{"component": "acme/worker", "version": "2"})

	ctx := replayStateTestContext()
	require.NoError(t, state.Apply(ctx, ChangeSet{{Kind: EntryUpdate, Entry: changed, OriginalEntry: &previous}}))
	require.NoError(t, state.Apply(ctx, ChangeSet{{Kind: EntryCreate, Entry: owner}}))
	require.NoError(t, state.Apply(ctx, ChangeSet{{Kind: EntryDelete, Entry: changed}}))

	selection, ok := state.DependencySelection("acme/app")
	require.True(t, ok)
	require.Equal(t, uint64(2), selection.Transaction)

	first, ok := state.FirstDependencyOperation(previous.ID, nil)
	require.True(t, ok)
	require.Equal(t, EntryUpdate, first.Kind)
	require.Equal(t, previous, *first.OriginalEntry)

	last, ok := state.LastDependencyOperation(previous.ID, nil)
	require.True(t, ok)
	require.Equal(t, EntryDelete, last.Kind)
	require.Equal(t, uint64(3), last.Transaction)
	require.True(t, state.IsTombstoned(previous.ID))
}

func TestReplayStateClassifiesKindlessDeleteAgainstBaseline(t *testing.T) {
	state := NewReplayState()
	id := NewID("app", "worker")
	require.NoError(t, state.Apply(context.Background(), ChangeSet{{Kind: EntryDelete, Entry: Entry{ID: id}}}))

	_, ok := state.LastDependencyOperation(id, nil)
	require.False(t, ok)

	baseline := State{{ID: id, Kind: NamespaceDependency}}
	last, ok := state.LastDependencyOperation(id, baseline)
	require.True(t, ok)
	require.Equal(t, EntryDelete, last.Kind)
	first, ok := state.FirstDependencyOperation(id, baseline)
	require.True(t, ok)
	require.Nil(t, first.OriginalEntry, "the missing original is legacy evidence")
	baseline[0].Data = payload.New(map[string]any{"component": "acme/app"})
	selection, ok, err := state.DependencySelectionWithBaseline("acme/app", baseline, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), selection.Transaction)
}

func TestReplayStateIgnoresOwnedNonDependencyOperations(t *testing.T) {
	state := NewReplayState()
	entry := Entry{ID: NewID("app", "config"), Kind: EntryKind, Registry: EntryMetadata{Owner: "acme/app"}}
	require.NoError(t, state.Apply(context.Background(), ChangeSet{{Kind: EntryUpdate, Entry: entry}}))
	_, ok := state.LastDependencyOperation(entry.ID, nil)
	require.False(t, ok)
}

func TestReplayStateUsesDeleteOriginalAsDependencyEvidence(t *testing.T) {
	state := NewReplayState()
	worker := Entry{ID: NewID("app", "worker"), Kind: NamespaceDependency,
		Registry: EntryMetadata{Owner: "acme/app"}, Data: payload.New(map[string]any{"component": "acme/worker"})}
	deleteOperation := Operation{Kind: EntryDelete, Entry: Entry{ID: worker.ID}, OriginalEntry: &worker}
	require.NoError(t, state.Apply(replayStateTestContext(), ChangeSet{deleteOperation}))

	operation, ok := state.LastDependencyOperation(worker.ID, nil)
	require.True(t, ok)
	require.Equal(t, worker, operation.Entry)
}

func TestReplayStateDoesNotReselectBaselineAfterRepeatedCompactDelete(t *testing.T) {
	state := NewReplayState()
	ownerID := NewID("deps", "app")
	worker := Entry{ID: NewID("app", "worker"), Kind: NamespaceDependency,
		Registry: EntryMetadata{Owner: "acme/app"}, Data: payload.New(map[string]any{"component": "acme/worker"})}
	baseline := State{{ID: ownerID, Kind: NamespaceDependency, Data: payload.New(map[string]any{"component": "acme/app"})}}

	require.NoError(t, state.Apply(context.Background(), ChangeSet{{Kind: EntryDelete, Entry: Entry{ID: ownerID}}}))
	require.NoError(t, state.Apply(context.Background(), ChangeSet{{Kind: EntryUpdate, Entry: worker}}))
	require.NoError(t, state.Apply(context.Background(), ChangeSet{{Kind: EntryDelete, Entry: Entry{ID: ownerID}}}))

	selection, ok, err := state.DependencySelectionWithBaseline("acme/app", baseline, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), selection.Transaction)
	last, ok := state.LastDependencyOperation(worker.ID, baseline)
	require.True(t, ok)
	require.Equal(t, uint64(2), last.Transaction)
}

func TestDependencyReplayStateReducesLegacyDependencyChanges(t *testing.T) {
	entry := Entry{ID: NewID("app", "worker"), Kind: EntryKind, Registry: EntryMetadata{Owner: "acme/app"}}
	ctx := WithDependencyBaseline(context.Background(), nil, []ChangeSet{{{Kind: EntryUpdate, Entry: entry}}})
	state, err := DependencyReplayState(ctx, nil)
	require.NoError(t, err)
	operation, ok := state.LastDependencyOperation(entry.ID, nil)
	require.True(t, ok)
	require.Equal(t, entry, operation.Entry)
}
