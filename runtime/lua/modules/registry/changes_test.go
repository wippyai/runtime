// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestCheckChangesValid(t *testing.T) {
	l := newTestState()
	defer l.Close()

	changes := &Changes{
		ops: []regapi.Operation{},
		log: zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = changes
	l.Push(ud)

	result := checkChanges(l)
	if result == nil {
		t.Error("expected non-nil changes")
	}
	if result != changes {
		t.Error("expected same changes instance")
	}
}

func TestChangesToString(t *testing.T) {
	l := newTestState()
	defer l.Close()

	changes := &Changes{
		ops: []regapi.Operation{
			{Kind: regapi.EntryCreate},
			{Kind: regapi.EntryUpdate},
		},
		log: zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = changes
	l.Push(ud)

	changesToString(l)

	result := l.Get(-1)
	str := string(result.(lua.LString))
	expected := "registry.Changes{ops=2}"
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}

func TestChangesToStringEmpty(t *testing.T) {
	l := newTestState()
	defer l.Close()

	changes := &Changes{
		ops: []regapi.Operation{},
		log: zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = changes
	l.Push(ud)

	changesToString(l)

	result := l.Get(-1)
	str := string(result.(lua.LString))
	expected := "registry.Changes{ops=0}"
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}

func TestDeleteIDsAcceptsEntryListsForBulkOverlayDelete(t *testing.T) {
	l := newTestState()
	defer l.Close()

	entries := l.CreateTable(2, 0)
	first := l.CreateTable(0, 1)
	first.RawSetString("id", lua.LString("runtime.env:host"))
	second := l.CreateTable(0, 2)
	second.RawSetString("ns", lua.LString("runtime.db"))
	second.RawSetString("name", lua.LString("source"))
	entries.RawSetInt(1, first)
	entries.RawSetInt(2, second)

	ids, err := deleteIDs(entries)
	if err != nil {
		t.Fatalf("deleteIDs returned error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected two IDs, got %d", len(ids))
	}
	if ids[0].String() != "runtime.env:host" || ids[1].String() != "runtime.db:source" {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

// A writer unaware of root status must not demote a deployment root. This is
// the shape keeper takes on every dependency update: read the entry, change a
// field, write it back. Absence of root on an update means unchanged.
func TestChangesUpdatePreservesStoredRoot(t *testing.T) {
	l := newTestState()
	defer l.Close()

	stored := regapi.Entry{
		ID:             regapi.ParseID("app.deps:keeper"),
		Kind:           "ns.dependency",
		Meta:           attrs.Bag{"module": "kickside/kickside"},
		DependencyRoot: true,
	}

	changes := &Changes{
		snapshot: &Snapshot{entries: []regapi.Entry{stored}, log: zap.NewNop()},
		ops:      []regapi.Operation{},
		log:      zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = changes
	l.Push(ud)

	entryTable := l.CreateTable(0, 3)
	entryTable.RawSetString("id", lua.LString("app.deps:keeper"))
	entryTable.RawSetString("kind", lua.LString("ns.dependency"))
	entryTable.RawSetString("meta", l.CreateTable(0, 0))
	l.Push(entryTable)

	changesUpdate(l)

	if len(changes.ops) != 1 {
		t.Fatalf("expected one op, got %d", len(changes.ops))
	}
	if !changes.ops[0].Entry.DependencyRoot {
		t.Error("expected an update that omits root to inherit the stored root status")
	}
}

func TestChangesUpdateHonoursExplicitDemotion(t *testing.T) {
	l := newTestState()
	defer l.Close()

	stored := regapi.Entry{
		ID:             regapi.ParseID("app.deps:keeper"),
		Kind:           "ns.dependency",
		DependencyRoot: true,
	}

	changes := &Changes{
		snapshot: &Snapshot{entries: []regapi.Entry{stored}, log: zap.NewNop()},
		ops:      []regapi.Operation{},
		log:      zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = changes
	l.Push(ud)

	entryTable := l.CreateTable(0, 3)
	entryTable.RawSetString("id", lua.LString("app.deps:keeper"))
	entryTable.RawSetString("kind", lua.LString("ns.dependency"))
	entryTable.RawSetString("root", lua.LFalse)
	l.Push(entryTable)

	changesUpdate(l)

	if len(changes.ops) != 1 {
		t.Fatalf("expected one op, got %d", len(changes.ops))
	}
	if changes.ops[0].Entry.DependencyRoot {
		t.Error("expected an explicit root=false to demote the entry")
	}
}
