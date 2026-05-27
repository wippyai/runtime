// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestSquashChangesets_CreateUpdate(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entryID := registry.NewID("test", "entry1")
	entry1 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{},
		Data: payload.NewString("initial"),
	}
	entry2 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{"updated": true},
		Data: payload.NewString("updated"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryCreate, Entry: entry1}},
		{{Kind: registry.EntryUpdate, Entry: entry2}},
	}

	squashed := builder.SquashChangesets(changesets)

	// Create + Update = Create with final value
	require.Len(t, squashed, 1)
	assert.Equal(t, registry.EntryCreate, squashed[0].Kind)
	assert.Equal(t, entry2, squashed[0].Entry)
}

func TestSquashChangesets_CreateDelete(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entryID := registry.NewID("test", "entry1")
	entry := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{},
		Data: payload.NewString("data"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryCreate, Entry: entry}},
		{{Kind: registry.EntryDelete, Entry: entry}},
	}

	squashed := builder.SquashChangesets(changesets)

	// Create + Delete = Nothing (cancel out)
	assert.Len(t, squashed, 0)
}

func TestSquashChangesets_UpdateUpdate(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entryID := registry.NewID("test", "entry1")
	entry1 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{"version": 1},
		Data: payload.NewString("v1"),
	}
	entry2 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{"version": 2},
		Data: payload.NewString("v2"),
	}
	entry3 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{"version": 3},
		Data: payload.NewString("v3"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryUpdate, Entry: entry1}},
		{{Kind: registry.EntryUpdate, Entry: entry2}},
		{{Kind: registry.EntryUpdate, Entry: entry3}},
	}

	squashed := builder.SquashChangesets(changesets)

	// Multiple Updates = Single Update with final value
	require.Len(t, squashed, 1)
	assert.Equal(t, registry.EntryUpdate, squashed[0].Kind)
	assert.Equal(t, entry3, squashed[0].Entry)
}

func TestSquashChangesets_UpdateDelete(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entryID := registry.NewID("test", "entry1")
	entry := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{},
		Data: payload.NewString("data"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryUpdate, Entry: entry}},
		{{Kind: registry.EntryDelete, Entry: entry}},
	}

	squashed := builder.SquashChangesets(changesets)

	// Update + Delete = Delete
	require.Len(t, squashed, 1)
	assert.Equal(t, registry.EntryDelete, squashed[0].Kind)
	assert.Equal(t, entry, squashed[0].Entry)
}

func TestSquashChangesets_DeleteCreate(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entryID := registry.NewID("test", "entry1")

	// Same kind - should become Update
	entry1 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{"old": true},
		Data: payload.NewString("old"),
	}
	entry2 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{"new": true},
		Data: payload.NewString("new"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryDelete, Entry: entry1}},
		{{Kind: registry.EntryCreate, Entry: entry2}},
	}

	squashed := builder.SquashChangesets(changesets)

	// Delete + Create (same kind) = Update
	require.Len(t, squashed, 1)
	assert.Equal(t, registry.EntryUpdate, squashed[0].Kind)
	assert.Equal(t, entry2, squashed[0].Entry)
}

func TestSquashChangesets_DeleteCreateDifferentKind(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entryID := registry.NewID("test", "entry1")

	// Different kinds - should remain as Create
	entry1 := registry.Entry{
		ID:   entryID,
		Kind: "service",
		Meta: map[string]any{},
		Data: payload.NewString("old"),
	}
	entry2 := registry.Entry{
		ID:   entryID,
		Kind: "component",
		Meta: map[string]any{},
		Data: payload.NewString("new"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryDelete, Entry: entry1}},
		{{Kind: registry.EntryCreate, Entry: entry2}},
	}

	squashed := builder.SquashChangesets(changesets)

	// Delete + Create (different kind) = Create
	require.Len(t, squashed, 1)
	assert.Equal(t, registry.EntryCreate, squashed[0].Kind)
	assert.Equal(t, entry2, squashed[0].Entry)
}

func TestSquashChangesets_ComplexSequence(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entry1ID := registry.NewID("test", "entry1")
	entry2ID := registry.NewID("test", "entry2")
	entry3ID := registry.NewID("test", "entry3")

	// Entry 1: Create -> Update -> Update -> Delete -> Create
	// Expected: Create (initial Create canceled by Delete, then new Create)
	entry1V1 := registry.Entry{
		ID:   entry1ID,
		Kind: "service",
		Data: payload.NewString("v1"),
	}
	entry1V2 := registry.Entry{
		ID:   entry1ID,
		Kind: "service",
		Data: payload.NewString("v2"),
	}
	entry1V3 := registry.Entry{
		ID:   entry1ID,
		Kind: "service",
		Data: payload.NewString("v3"),
	}
	entry1V4 := registry.Entry{
		ID:   entry1ID,
		Kind: "service",
		Data: payload.NewString("v4"),
	}

	// Entry 2: Create -> Update -> Delete
	// Expected: Nothing (canceled out)
	entry2V1 := registry.Entry{
		ID:   entry2ID,
		Kind: "component",
		Data: payload.NewString("v1"),
	}
	entry2V2 := registry.Entry{
		ID:   entry2ID,
		Kind: "component",
		Data: payload.NewString("v2"),
	}

	// Entry 3: Update only
	// Expected: Update
	entry3 := registry.Entry{
		ID:   entry3ID,
		Kind: "task",
		Data: payload.NewString("data"),
	}

	changesets := []registry.ChangeSet{
		// Version 1 changes
		{
			{Kind: registry.EntryCreate, Entry: entry1V1},
			{Kind: registry.EntryCreate, Entry: entry2V1},
		},
		// Version 2 changes
		{
			{Kind: registry.EntryUpdate, Entry: entry1V2},
			{Kind: registry.EntryUpdate, Entry: entry2V2},
			{Kind: registry.EntryUpdate, Entry: entry3},
		},
		// Version 3 changes
		{
			{Kind: registry.EntryUpdate, Entry: entry1V3},
			{Kind: registry.EntryDelete, Entry: entry2V2},
		},
		// Version 4 changes
		{
			{Kind: registry.EntryDelete, Entry: entry1V3},
		},
		// Version 5 changes
		{
			{Kind: registry.EntryCreate, Entry: entry1V4},
		},
	}

	squashed := builder.SquashChangesets(changesets)

	// Check results
	assert.Len(t, squashed, 2) // entry1 and entry3 remain

	// Find each entry in the result
	var foundEntry1, foundEntry3 bool
	for _, op := range squashed {
		switch op.Entry.ID {
		case entry1ID:
			foundEntry1 = true
			// Create -> Update -> Update -> Delete -> Create = Create
			// (The initial Create and intermediate Updates are canceled by Delete,
			// then a new Create is added, resulting in just Create)
			assert.Equal(t, registry.EntryCreate, op.Kind)
			assert.Equal(t, entry1V4, op.Entry)
		case entry2ID:
			t.Error("Entry 2 should have been canceled out (Create -> Update -> Delete)")
		case entry3ID:
			foundEntry3 = true
			assert.Equal(t, registry.EntryUpdate, op.Kind)
			assert.Equal(t, entry3, op.Entry)
		}
	}

	assert.True(t, foundEntry1, "Entry 1 should be present")
	assert.True(t, foundEntry3, "Entry 3 should be present")
}

func TestSquashChangesets_EmptyInput(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	// Empty changesets
	squashed := builder.SquashChangesets([]registry.ChangeSet{})
	assert.Len(t, squashed, 0)

	// Changesets with no operations
	squashed = builder.SquashChangesets([]registry.ChangeSet{{}, {}, {}})
	assert.Len(t, squashed, 0)
}

func TestSquashChangesets_SingleOperation(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entry := registry.Entry{
		ID:   registry.NewID("test", "entry1"),
		Kind: "service",
		Data: payload.NewString("data"),
	}

	changesets := []registry.ChangeSet{
		{{Kind: registry.EntryCreate, Entry: entry}},
	}

	squashed := builder.SquashChangesets(changesets)

	require.Len(t, squashed, 1)
	assert.Equal(t, registry.EntryCreate, squashed[0].Kind)
	assert.Equal(t, entry, squashed[0].Entry)
}

func TestSquashChangesets_MultipleEntries(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	// Test that operations on different entries don't interfere
	entry1 := registry.Entry{
		ID:   registry.NewID("test", "entry1"),
		Kind: "service",
		Data: payload.NewString("data1"),
	}
	entry2 := registry.Entry{
		ID:   registry.NewID("test", "entry2"),
		Kind: "service",
		Data: payload.NewString("data2"),
	}
	entry3 := registry.Entry{
		ID:   registry.NewID("test", "entry3"),
		Kind: "service",
		Data: payload.NewString("data3"),
	}

	changesets := []registry.ChangeSet{
		{
			{Kind: registry.EntryCreate, Entry: entry1},
			{Kind: registry.EntryCreate, Entry: entry2},
		},
		{
			{Kind: registry.EntryUpdate, Entry: entry1},
			{Kind: registry.EntryDelete, Entry: entry2},
			{Kind: registry.EntryCreate, Entry: entry3},
		},
	}

	squashed := builder.SquashChangesets(changesets)

	// entry1: Create + Update = Create
	// entry2: Create + Delete = Nothing
	// entry3: Create = Create
	assert.Len(t, squashed, 2) // Only entry1 and entry3

	foundEntry1 := false
	foundEntry3 := false
	for _, op := range squashed {
		switch op.Entry.ID.Name {
		case "entry1":
			foundEntry1 = true
			assert.Equal(t, registry.EntryCreate, op.Kind)
		case "entry2":
			t.Error("Entry 2 should have been canceled out")
		case "entry3":
			foundEntry3 = true
			assert.Equal(t, registry.EntryCreate, op.Kind)
		}
	}

	assert.True(t, foundEntry1)
	assert.True(t, foundEntry3)
}

func TestSquashChangesets_DeleteOpsDependentsBeforeDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	repoID := registry.NewID("app", "repo")
	handlerID := registry.NewID("app", "handler")

	repo := registry.Entry{
		ID:   repoID,
		Kind: "library.lua",
		Meta: map[string]any{},
	}
	handler := registry.Entry{
		ID:   handlerID,
		Kind: "function.lua",
		Meta: map[string]any{
			registry.TagDependsOn: []string{repoID.String()},
		},
	}

	squashed := builder.SquashChangesets([]registry.ChangeSet{
		{
			{Kind: registry.EntryDelete, Entry: repo},
			{Kind: registry.EntryDelete, Entry: handler},
		},
	})

	require.Len(t, squashed, 2)
	assert.Equal(t, registry.EntryDelete, squashed[0].Kind)
	assert.Equal(t, handlerID, squashed[0].Entry.ID)
	assert.Equal(t, registry.EntryDelete, squashed[1].Kind)
	assert.Equal(t, repoID, squashed[1].Entry.ID)
}

func TestReverseChangeset(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entry1 := registry.Entry{
		ID:   registry.NewID("test", "entry1"),
		Kind: "service",
		Data: payload.NewString("original"),
	}
	entry1Updated := registry.Entry{
		ID:   registry.NewID("test", "entry1"),
		Kind: "service",
		Data: payload.NewString("updated"),
	}
	entry2 := registry.Entry{
		ID:   registry.NewID("test", "entry2"),
		Kind: "component",
		Data: payload.NewString("data"),
	}

	// Changeset that created entry2 and updated entry1 (with OriginalEntry populated)
	changeset := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: entry2},
		{Kind: registry.EntryUpdate, Entry: entry1Updated, OriginalEntry: &entry1},
	}

	reversed, err := builder.ReverseChangeset(changeset)
	require.NoError(t, err)

	// Reversed should delete entry2 and restore entry1 to original
	require.Len(t, reversed, 2)

	// Operations are processed in reverse order
	assert.Equal(t, registry.EntryUpdate, reversed[0].Kind) // Was Update, reverse is Update with old value
	assert.Equal(t, entry1, reversed[0].Entry)

	assert.Equal(t, registry.EntryDelete, reversed[1].Kind) // Was Create, reverse is Delete
	assert.Equal(t, entry2, reversed[1].Entry)
}

func TestReverseChangeset_Delete(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	entry := registry.Entry{
		ID:   registry.NewID("test", "entry1"),
		Kind: "service",
		Data: payload.NewString("data"),
	}

	// Changeset that deleted the entry (with OriginalEntry populated)
	changeset := registry.ChangeSet{
		{Kind: registry.EntryDelete, Entry: entry, OriginalEntry: &entry},
	}

	reversed, err := builder.ReverseChangeset(changeset)
	require.NoError(t, err)

	// Reversed should recreate the entry
	require.Len(t, reversed, 1)
	assert.Equal(t, registry.EntryCreate, reversed[0].Kind)
	assert.Equal(t, entry, reversed[0].Entry)
}
