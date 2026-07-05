// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestEventOutdatedConstant(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	event := l.GetGlobal("process").(*lua.LTable).RawGetString("event").(*lua.LTable)
	got := event.RawGetString("OUTDATED")
	if got.Type() != lua.LTString || got.String() != "pid.outdated" {
		t.Fatalf("expected process.event.OUTDATED = pid.outdated, got %v", got)
	}
}

func TestGetOptions_IncludesUpgradable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	// Without a process context, get_options still reports the option, default false.
	err := l.DoString(`
		local opts = process.get_options()
		if type(opts) ~= "table" then error("expected table") end
		if opts.upgradable ~= false then error("expected upgradable=false, got " .. tostring(opts.upgradable)) end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
