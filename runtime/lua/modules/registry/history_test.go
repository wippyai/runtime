// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"go.uber.org/zap"
)

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
