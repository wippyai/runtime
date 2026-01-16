package registry

import (
	"testing"

	regapi "github.com/wippyai/runtime/api/registry"
	lua "github.com/yuin/gopher-lua"
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
	id   uint
	str  string
	prev regapi.Version
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
