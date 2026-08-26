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

func TestSnapshotStateReturnsTotalDetachedState(t *testing.T) {
	rootID := regapi.NewID("app", "root")
	ownedID := regapi.NewID("pkg", "entry")
	snap := &Snapshot{
		entries: regapi.State{
			{ID: rootID, Kind: regapi.NamespaceDependency},
			{ID: ownedID, Kind: "function.lua"},
		},
		prov: regapi.ProvenanceMap{
			rootID:  {Root: true},
			ownedID: {Module: "org/pkg", Version: "1.2.3", Digest: "sha256:abc"},
		},
		log: zap.NewNop(),
	}

	runSnapshotState(setupContextWithTranscoder(), t, snap, `
		local first, err = snap:state()
		assert(err == nil and #first.entries == 2)
		assert(first.entries[1].root == true)
		assert(first.entries[1].meta.module == nil)
		assert(first.provenance["app:root"].module == "")
		assert(first.provenance["app:root"].root == true)
		assert(first.provenance["pkg:entry"].module == "org/pkg")
		assert(first.provenance["pkg:entry"].version == "1.2.3")
		assert(first.provenance["pkg:entry"].digest == "sha256:abc")

		first.entries[1].root = false
		first.entries[1].meta.module = "forged/pkg"
		first.provenance["pkg:entry"].module = "forged/pkg"
		first.provenance["app:root"] = nil

		local second, second_err = snap:state()
		assert(second_err == nil and #second.entries == 2)
		assert(second.entries[1].root == true)
		assert(second.entries[1].meta.module == nil)
		assert(second.provenance["pkg:entry"].module == "org/pkg")
		assert(second.provenance["app:root"].root == true)
	`)
}

func TestSnapshotStateRejectsInvalidProvenance(t *testing.T) {
	id := regapi.NewID("app", "entry")
	assertInvalid := func(t *testing.T, provenance regapi.ProvenanceMap) {
		t.Helper()
		snap := &Snapshot{entries: regapi.State{{ID: id}}, prov: provenance, log: zap.NewNop()}
		runSnapshotState(setupContextWithTranscoder(), t, snap, `
			local state, err = snap:state()
			assert(state == nil and err ~= nil)
			assert(err:kind() == errors.INTERNAL)
		`)
	}
	t.Run("missing", func(t *testing.T) {
		assertInvalid(t, regapi.ProvenanceMap{})
	})
	t.Run("orphaned", func(t *testing.T) {
		assertInvalid(t, regapi.ProvenanceMap{id: {}, regapi.NewID("app", "orphan"): {}})
	})
}

func TestSnapshotStateFiltersEntriesAndProvenanceTogether(t *testing.T) {
	visible := regapi.NewID("app", "visible")
	hidden := regapi.NewID("app", "hidden")
	snap := &Snapshot{
		entries: regapi.State{{ID: visible}, {ID: hidden}},
		prov: regapi.ProvenanceMap{
			visible: {Module: "org/visible"},
			hidden:  {Module: "org/hidden"},
		},
		log: zap.NewNop(),
	}
	ctx, release := strictOverlayContext(t, "registry.get\x00"+visible.String())
	defer release()

	runSnapshotState(ctx, t, snap, `
		local state, err = snap:state()
		assert(err == nil and #state.entries == 1)
		assert(state.entries[1].id == "app:visible")
		assert(state.provenance["app:visible"].module == "org/visible")
		assert(state.provenance["app:hidden"] == nil)
	`)
}

func TestOverlaySnapshotStateRequiresProvenance(t *testing.T) {
	const owner = "runtime:overlay"
	snap := &Snapshot{
		overlayOwner: owner,
		entries:      regapi.State{{ID: regapi.NewID("app", "draft")}},
		log:          zap.NewNop(),
	}
	ctx, release := strictOverlayContext(t, "registry.overlay.get\x00"+owner)
	defer release()

	runSnapshotState(ctx, t, snap, `
		local state, err = snap:state()
		assert(state == nil and err ~= nil)
		assert(err:kind() == errors.UNAVAILABLE)
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
