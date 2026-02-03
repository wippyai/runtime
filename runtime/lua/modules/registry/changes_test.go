package registry

import (
	"testing"

	lua "github.com/wippyai/go-lua"
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
