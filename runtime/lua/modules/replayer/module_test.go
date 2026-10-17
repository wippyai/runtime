// SPDX-License-Identifier: MPL-2.0

package replayer

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func newBound(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestBind(t *testing.T) {
	l := newBound(t)
	defer l.Close()

	mod := l.GetGlobal("replayer")
	if mod.Type() != lua.LTTable {
		t.Fatal("replayer module not registered")
	}
	if mod.(*lua.LTable).RawGetString("replay_json_file").Type() != lua.LTFunction {
		t.Error("replay_json_file function not registered")
	}
}

// Argument validation runs before any context/Temporal access, so this needs no runtime.
func TestReplayBadArgs(t *testing.T) {
	l := newBound(t)
	defer l.Close()

	err := l.DoString(`
		local _, err = replayer.replay_json_file("", "history.json")
		if err == nil then error("expected error for empty workflow id") end

		_, err = replayer.replay_json_file("no-colon-here", "history.json")
		if err == nil then error("expected error for non ns:name id") end

		_, err = replayer.replay_json_file("app:wf", "")
		if err == nil then error("expected error for empty path") end
	`)
	if err != nil {
		t.Errorf("bad-args test failed: %v", err)
	}
}

// Well-formed args with no Temporal in context must fail gracefully, not panic.
func TestReplayNoTemporalContext(t *testing.T) {
	l := newBound(t)
	defer l.Close()

	err := l.DoString(`
		local ok, err = replayer.replay_json_file("app.letter:scheduled_delivery_workflow", "history.json")
		if ok ~= nil then error("expected nil result without temporal context") end
		if err == nil then error("expected error without temporal context") end
	`)
	if err != nil {
		t.Errorf("no-temporal-context test failed: %v", err)
	}
}
