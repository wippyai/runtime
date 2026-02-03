package registry

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestVersionID(t *testing.T) {
	l := newTestState()
	defer l.Close()

	mock := &mockVersion{id: 123, str: "v123"}
	ud := l.NewUserData()
	ud.Value = mock
	l.Push(ud)

	versionID(l)

	result := l.Get(-1)
	num, ok := result.(lua.LNumber)
	if !ok {
		t.Fatalf("expected LNumber, got %T", result)
	}

	if uint(num) != 123 {
		t.Errorf("expected 123, got %d", uint(num))
	}
}

func TestVersionPreviousExists(t *testing.T) {
	l := newTestState()
	defer l.Close()

	prev := &mockVersion{id: 1, str: "v1"}
	current := &mockVersion{id: 2, str: "v2", prev: prev}

	ud := l.NewUserData()
	ud.Value = current
	l.Push(ud)

	versionPrevious(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestVersionPreviousNil(t *testing.T) {
	l := newTestState()
	defer l.Close()

	current := &mockVersion{id: 1, str: "v1", prev: nil}

	ud := l.NewUserData()
	ud.Value = current
	l.Push(ud)

	versionPrevious(l)

	result := l.Get(-1)
	if result != lua.LNil {
		t.Errorf("expected LNil, got %v", result)
	}
}

func TestVersionString(t *testing.T) {
	l := newTestState()
	defer l.Close()

	mock := &mockVersion{id: 42, str: "version-42"}
	ud := l.NewUserData()
	ud.Value = mock
	l.Push(ud)

	versionString(l)

	result := l.Get(-1)
	str, ok := result.(lua.LString)
	if !ok {
		t.Fatalf("expected LString, got %T", result)
	}

	if str != "version-42" {
		t.Errorf("expected 'version-42', got %s", str)
	}
}

func TestVersionToString(t *testing.T) {
	l := newTestState()
	defer l.Close()

	mock := &mockVersion{id: 5, str: "v5"}
	ud := l.NewUserData()
	ud.Value = mock
	l.Push(ud)

	versionToString(l)

	result := l.Get(-1)
	str := string(result.(lua.LString))
	expected := "registry.Version{v5}"
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}
