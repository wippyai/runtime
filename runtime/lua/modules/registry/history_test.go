// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"go.uber.org/zap"
)

type lookupOnlyHistory struct {
	*historymem.Storage
}

func (h *lookupOnlyHistory) Versions() ([]regapi.Version, error) {
	panic("findHistoryVersion enumerated a lookup-capable history")
}

func TestCheckHistoryValid(t *testing.T) {
	l := newTestState()
	defer l.Close()

	history := &History{
		log: zap.NewNop(),
	}

	ud := l.NewUserData()
	ud.Value = history
	l.Push(ud)

	result := checkHistory(l)
	if result == nil {
		t.Error("expected non-nil history")
	}
	if result != history {
		t.Error("expected same history instance")
	}
}

func TestHistoryToString(t *testing.T) {
	l := newTestState()
	defer l.Close()

	historyToString(l)

	result := l.Get(-1)
	str := string(result.(lua.LString))
	expected := "registry.History{}"
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}

func TestFindHistoryVersionUsesDirectLookup(t *testing.T) {
	storage := historymem.New()
	v1 := version.FromParent(version.New(regapi.RootVersion), 1)
	if err := storage.Save(v1, nil, false); err != nil {
		t.Fatal(err)
	}

	got, err := findHistoryVersion(&lookupOnlyHistory{Storage: storage}, v1.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID() != v1.ID() {
		t.Fatalf("direct lookup returned %#v", got)
	}
}
