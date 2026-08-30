// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"go.uber.org/zap"
)

func newTestState() *lua.LState {
	return lua.NewState()
}

func TestSnapshotGetAllEntries(t *testing.T) {
	entries := []regapi.Entry{
		{ID: regapi.ID{NS: "test", Name: "entry1"}},
		{ID: regapi.ID{NS: "test", Name: "entry2"}},
	}

	snap := &Snapshot{
		entries: entries,
		log:     zap.NewNop(),
	}

	result, err := snap.GetAllEntries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
}

func TestSnapshotGetEntrySuccess(t *testing.T) {
	id := regapi.ID{NS: "test", Name: "entry1"}
	entries := []regapi.Entry{
		{ID: id, Kind: "test-kind"},
	}

	snap := &Snapshot{
		entries: entries,
		log:     zap.NewNop(),
	}

	result, err := snap.GetEntry(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != id {
		t.Errorf("expected id %v, got %v", id, result.ID)
	}
}

func TestSnapshotGetEntryNotFound(t *testing.T) {
	entries := []regapi.Entry{
		{ID: regapi.ID{NS: "test", Name: "entry1"}},
	}

	snap := &Snapshot{
		entries: entries,
		log:     zap.NewNop(),
	}

	id := regapi.ID{NS: "test", Name: "missing"}
	_, err := snap.GetEntry(id)
	if err == nil {
		t.Error("expected error for missing entry")
	}
}

func runSnapshotState(ctx context.Context, t *testing.T, snap *Snapshot, source string) {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	value.PushTypedUserData(l, snap, typeSnapshot)
	l.SetGlobal("snap", l.Get(-1))
	l.Pop(1)
	require.NoError(t, l.DoString(source))
}

func TestSnapshotStateReturnsDetachedRegistryMetadataAndResolution(t *testing.T) {
	rootID := regapi.NewID("app.deps", "module")
	ownedID := regapi.NewID("app", "handler")
	snap := &Snapshot{
		entries: regapi.State{
			{ID: rootID, Kind: regapi.NamespaceDependency, Registry: regapi.EntryMetadata{Root: true}},
			{ID: ownedID, Kind: "function.lua", Registry: regapi.EntryMetadata{Owner: "org/module"}},
		},
		state: regapi.StateMetadata{Resolution: &regapi.DependencyResolution{
			Digest:      "sha256:resolution",
			InputDigest: "sha256:inputs",
			Roots: []regapi.DependencyRoot{{
				ID: rootID.String(), Component: "org/module", Version: "^1.0",
			}},
			Modules: []regapi.ResolvedModule{{
				Name: "org/module", Version: "1.2.3", VersionID: "version-id", Source: "hub", Digest: "sha256:module", SizeBytes: 42, Protected: true,
			}},
		}},
		log: zap.NewNop(),
	}

	runSnapshotState(setupContextWithTranscoder(), t, snap, `
		local first, err = snap:state()
		assert(err == nil and #first.entries == 2)
		assert(first.entries[1].registry.owner == "")
		assert(first.entries[1].registry.root == true)
		assert(first.entries[2].registry.owner == "org/module")
		assert(first.entries[2].registry.root == false)
		assert(first.entries[2].root == nil)
		assert(first.resolution.digest == "sha256:resolution")
		assert(first.resolution.roots[1].component == "org/module")
		assert(first.resolution.modules[1].version == "1.2.3")
		assert(first.resolution.modules[1].size_bytes == 42)

		first.entries[2].registry.owner = "forged/module"
		first.entries[1].registry.root = false
		first.resolution.modules[1].version = "999.0.0"

		local second, second_err = snap:state()
		assert(second_err == nil)
		assert(second.entries[2].registry.owner == "org/module")
		assert(second.entries[1].registry.root == true)
		assert(second.resolution.modules[1].version == "1.2.3")
	`)
}

func TestSnapshotStateFiltersEntriesWithMetadata(t *testing.T) {
	visible := regapi.NewID("app", "visible")
	hidden := regapi.NewID("app", "hidden")
	snap := &Snapshot{
		entries: regapi.State{
			{ID: visible, Registry: regapi.EntryMetadata{Owner: "org/visible"}},
			{ID: hidden, Registry: regapi.EntryMetadata{Owner: "org/hidden"}},
		},
		log: zap.NewNop(),
	}
	ctx, release := strictOverlayContext(t, "registry.get\x00"+visible.String())
	defer release()

	runSnapshotState(ctx, t, snap, `
		local state, err = snap:state()
		assert(err == nil and #state.entries == 1)
		assert(state.entries[1].id == "app:visible")
		assert(state.entries[1].registry.owner == "org/visible")
	`)
}

func TestCheckSnapshotValid(t *testing.T) {
	l := newTestState()
	defer l.Close()

	snap := &Snapshot{
		entries: []regapi.Entry{},
		log:     zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = snap
	l.Push(ud)

	result := checkSnapshot(l)
	if result == nil {
		t.Error("expected non-nil snapshot")
	}
	if result != snap {
		t.Error("expected same snapshot instance")
	}
}

func TestSnapshotToString(t *testing.T) {
	l := newTestState()
	defer l.Close()

	mockVersion := &mockVersion{id: 42, str: "v42"}
	snap := &Snapshot{
		version: mockVersion,
		entries: []regapi.Entry{},
		log:     zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = snap
	l.Push(ud)

	snapshotToString(l)

	result := l.Get(-1)
	str, ok := result.(lua.LString)
	if !ok {
		t.Fatalf("expected LString, got %T", result)
	}

	expected := lua.LString("registry.Snapshot{version=v42}")
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}

type mockVersion struct {
	prev regapi.Version
	str  string
	id   uint
}

func (m *mockVersion) ID() uint {
	return m.id
}

func (m *mockVersion) String() string {
	return m.str
}

func (m *mockVersion) Previous() regapi.Version {
	return m.prev
}

func (m *mockVersion) Next() regapi.Version {
	return nil
}
