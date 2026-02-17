package system

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/security"
)

func hostsList(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "hosts", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on hosts").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	inspector := process.GetInspector(l.Context())
	if inspector == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "host inspector not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	hosts := inspector.ListHosts()
	result := l.CreateTable(len(hosts), 0)

	for i, h := range hosts {
		t := l.CreateTable(0, 6)
		t.RawSetString("id", lua.LString(h.ID.String()))
		t.RawSetString("workers", lua.LNumber(h.Workers))
		t.RawSetString("processes", lua.LNumber(h.Processes))
		t.RawSetString("executed", lua.LNumber(h.Executed))
		t.RawSetString("stolen", lua.LNumber(h.Stolen))
		t.RawSetString("queue_depth", lua.LNumber(h.QueueDepth))
		result.RawSetInt(i+1, t)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

func hostsProcesses(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "hosts", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on hosts").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	hostIDStr := l.CheckString(1)
	if hostIDStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "host ID required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	inspector := process.GetInspector(l.Context())
	if inspector == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "host inspector not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	hostID := registry.ParseID(hostIDStr)
	procs := inspector.HostProcesses(hostID)
	result := l.CreateTable(len(procs), 0)

	for i, p := range procs {
		t := l.CreateTable(0, 8)
		t.RawSetString("pid", lua.LString(p.PID.String()))
		t.RawSetString("host", lua.LString(p.Host.String()))
		t.RawSetString("source", lua.LString(p.Source.String()))
		t.RawSetString("state", lua.LString(p.State))
		t.RawSetString("steps", lua.LNumber(p.Steps))
		t.RawSetString("started_at", lua.LNumber(p.StartedAt))
		if p.Parent.UniqID != "" {
			t.RawSetString("parent", lua.LString(p.Parent.String()))
		}

		if p.Stats != nil {
			if bag, ok := p.Stats.(attrs.Bag); ok {
				statsTable := l.CreateTable(0, bag.Len())
				bag.Iterate(func(key string, val any) {
					if lv, err := luapayload.GoToLua(val); err == nil {
						statsTable.RawSetString(key, lv)
					}
				})
				t.RawSetString("stats", statsTable)
			}
		}

		result.RawSetInt(i+1, t)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
