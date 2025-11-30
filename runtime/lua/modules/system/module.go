package system

import (
	"math"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *lua2api.Registration
	initOnce     sync.Once
	settingsMu   sync.Mutex
)

// Module is the singleton system module instance.
var Module = &systemModule{}

type systemModule struct{}

func (m *systemModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "system",
		Description: "System memory, GC, and process info",
		Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	}
}

func (m *systemModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *systemModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 5)

	mod.RawSetString("memory", createMemoryTable())
	mod.RawSetString("gc", createGCTable())
	mod.RawSetString("runtime", createRuntimeTable())
	mod.RawSetString("process", createProcessTable())

	mod.Immutable = true
	return mod
}

func createMemoryTable() *lua.LTable {
	t := lua.CreateTable(0, 5)
	t.RawSetString("stats", lua.LGoFunc(memStats))
	t.RawSetString("allocated", lua.LGoFunc(allocated))
	t.RawSetString("heap_objects", lua.LGoFunc(heapObjects))
	t.RawSetString("set_limit", lua.LGoFunc(setMemoryLimit))
	t.RawSetString("get_limit", lua.LGoFunc(getMemoryLimit))
	t.Immutable = true
	return t
}

func createGCTable() *lua.LTable {
	t := lua.CreateTable(0, 3)
	t.RawSetString("collect", lua.LGoFunc(gcCollect))
	t.RawSetString("set_percent", lua.LGoFunc(setGCPercent))
	t.RawSetString("get_percent", lua.LGoFunc(getGCPercent))
	t.Immutable = true
	return t
}

func createRuntimeTable() *lua.LTable {
	t := lua.CreateTable(0, 3)
	t.RawSetString("goroutines", lua.LGoFunc(numGoroutines))
	t.RawSetString("max_procs", lua.LGoFunc(goMaxProcs))
	t.RawSetString("cpu_count", lua.LGoFunc(numCPU))
	t.Immutable = true
	return t
}

func createProcessTable() *lua.LTable {
	t := lua.CreateTable(0, 2)
	t.RawSetString("pid", lua.LGoFunc(pid))
	t.RawSetString("hostname", lua.LGoFunc(hostname))
	t.Immutable = true
	return t
}

func memStats(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)

	t := l.CreateTable(0, 15)
	t.RawSetString("alloc", lua.LNumber(ms.Alloc))
	t.RawSetString("total_alloc", lua.LNumber(ms.TotalAlloc))
	t.RawSetString("sys", lua.LNumber(ms.Sys))
	t.RawSetString("heap_alloc", lua.LNumber(ms.HeapAlloc))
	t.RawSetString("heap_sys", lua.LNumber(ms.HeapSys))
	t.RawSetString("heap_idle", lua.LNumber(ms.HeapIdle))
	t.RawSetString("heap_in_use", lua.LNumber(ms.HeapInuse))
	t.RawSetString("heap_released", lua.LNumber(ms.HeapReleased))
	t.RawSetString("heap_objects", lua.LNumber(ms.HeapObjects))
	t.RawSetString("stack_in_use", lua.LNumber(ms.StackInuse))
	t.RawSetString("stack_sys", lua.LNumber(ms.StackSys))
	t.RawSetString("mspan_in_use", lua.LNumber(ms.MSpanInuse))
	t.RawSetString("mspan_sys", lua.LNumber(ms.MSpanSys))
	t.RawSetString("num_gc", lua.LNumber(ms.NumGC))
	t.RawSetString("next_gc", lua.LNumber(ms.NextGC))

	l.Push(t)
	l.Push(lua.LNil)
	return 2
}

func allocated(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)

	l.Push(lua.LNumber(ms.Alloc))
	l.Push(lua.LNil)
	return 2
}

func heapObjects(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)

	l.Push(lua.LNumber(ms.HeapObjects))
	l.Push(lua.LNil)
	return 2
}

func setMemoryLimit(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.control", "memory_limit", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("memory limit required"))
		return 2
	}

	limit := l.CheckInt64(1)
	if limit < 0 {
		if limit == -1 {
			limit = math.MaxInt64
		} else {
			l.Push(lua.LNil)
			l.Push(lua.LString("limit must be non-negative or -1"))
			return 2
		}
	}

	settingsMu.Lock()
	prev := debug.SetMemoryLimit(limit)
	settingsMu.Unlock()

	l.Push(lua.LNumber(prev))
	l.Push(lua.LNil)
	return 2
}

func getMemoryLimit(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory_limit", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	settingsMu.Lock()
	current := debug.SetMemoryLimit(-1)
	debug.SetMemoryLimit(current)
	settingsMu.Unlock()

	l.Push(lua.LNumber(current))
	l.Push(lua.LNil)
	return 2
}

func gcCollect(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.gc", "gc", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	goruntime.GC()

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func setGCPercent(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.gc", "gc_percent", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("percent required"))
		return 2
	}

	percent := l.CheckInt(1)

	settingsMu.Lock()
	old := debug.SetGCPercent(percent)
	settingsMu.Unlock()

	if old < 0 {
		old = 100
	}

	l.Push(lua.LNumber(old))
	l.Push(lua.LNil)
	return 2
}

func getGCPercent(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "gc_percent", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	settingsMu.Lock()
	orig := debug.SetGCPercent(-1)
	debug.SetGCPercent(orig)
	settingsMu.Unlock()

	if orig < 0 {
		orig = 100
	}

	l.Push(lua.LNumber(orig))
	l.Push(lua.LNil)
	return 2
}

func numGoroutines(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "goroutines", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	l.Push(lua.LNumber(goruntime.NumGoroutine()))
	l.Push(lua.LNil)
	return 2
}

func goMaxProcs(l *lua.LState) int {
	if l.GetTop() > 0 {
		if !security.IsAllowed(l.Context(), "system.control", "gomaxprocs", nil) {
			l.Push(lua.LNil)
			l.Push(lua.LString("permission denied"))
			return 2
		}
		n := l.CheckInt(1)
		if n <= 0 {
			l.Push(lua.LNil)
			l.Push(lua.LString("value must be positive"))
			return 2
		}
		prev := goruntime.GOMAXPROCS(n)
		l.Push(lua.LNumber(prev))
	} else {
		if !security.IsAllowed(l.Context(), "system.read", "gomaxprocs", nil) {
			l.Push(lua.LNil)
			l.Push(lua.LString("permission denied"))
			return 2
		}
		l.Push(lua.LNumber(goruntime.GOMAXPROCS(0)))
	}
	l.Push(lua.LNil)
	return 2
}

func numCPU(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "cpu", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	l.Push(lua.LNumber(goruntime.NumCPU()))
	l.Push(lua.LNil)
	return 2
}

func pid(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "pid", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	l.Push(lua.LNumber(os.Getpid()))
	l.Push(lua.LNil)
	return 2
}

func hostname(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "hostname", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied"))
		return 2
	}

	name, err := os.Hostname()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(name))
	l.Push(lua.LNil)
	return 2
}
