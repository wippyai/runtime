// SPDX-License-Identifier: MPL-2.0

package system

import (
	"math"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"sync"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/runtime/security"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
	settingsMu  sync.Mutex
)

func initModuleTable() {
	mod := lua.CreateTable(0, 12)

	mod.RawSetString("memory", createMemoryTable())
	mod.RawSetString("gc", createGCTable())
	mod.RawSetString("runtime", createRuntimeTable())
	mod.RawSetString("process", createProcessTable())
	mod.RawSetString("node", createNodeTable())
	mod.RawSetString("cluster", createClusterTable())
	mod.RawSetString("raft", createRaftTable())
	mod.RawSetString("lock", createLockTable())
	mod.RawSetString("supervisor", createSupervisorTable())
	mod.RawSetString("hosts", createHostsTable())
	mod.RawSetString("exit", lua.LGoFunc(exit))
	mod.RawSetString("modules", lua.LGoFunc(modules))

	mod.Immutable = true
	moduleTable = mod
}

// Module is the system module definition.
var Module = &luaapi.ModuleDef{
	Name:        "system",
	Description: "System runtime, process, and cluster (node/cluster/raft/lock) introspection",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initOnce.Do(initModuleTable)
		return moduleTable, nil
	},
	Types: ModuleTypes,
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
	t := lua.CreateTable(0, 3)
	t.RawSetString("pid", lua.LGoFunc(pid))
	t.RawSetString("hostname", lua.LGoFunc(hostname))
	t.RawSetString("cwd", lua.LGoFunc(cwd))
	t.Immutable = true
	return t
}

func createNodeTable() *lua.LTable {
	t := lua.CreateTable(0, 3)
	t.RawSetString("id", lua.LGoFunc(nodeID))
	t.RawSetString("addr", lua.LGoFunc(nodeAddr))
	t.RawSetString("role", lua.LGoFunc(nodeRole))
	t.Immutable = true
	return t
}

func createClusterTable() *lua.LTable {
	t := lua.CreateTable(0, 3)
	t.RawSetString("members", lua.LGoFunc(clusterMembers))
	t.RawSetString("leader", lua.LGoFunc(clusterLeader))
	t.RawSetString("size", lua.LGoFunc(clusterSize))
	t.Immutable = true
	return t
}

func createRaftTable() *lua.LTable {
	t := lua.CreateTable(0, 6)
	t.RawSetString("is_leader", lua.LGoFunc(raftIsLeader))
	t.RawSetString("is_member", lua.LGoFunc(raftIsMember))
	t.RawSetString("role", lua.LGoFunc(raftRole))
	t.RawSetString("term", lua.LGoFunc(raftTerm))
	t.RawSetString("commit_index", lua.LGoFunc(raftCommitIndex))
	t.RawSetString("stats", lua.LGoFunc(raftStats))
	t.Immutable = true
	return t
}

func createSupervisorTable() *lua.LTable {
	t := lua.CreateTable(0, 2)
	t.RawSetString("state", lua.LGoFunc(supervisorState))
	t.RawSetString("states", lua.LGoFunc(supervisorStates))
	t.Immutable = true
	return t
}

func createHostsTable() *lua.LTable {
	t := lua.CreateTable(0, 2)
	t.RawSetString("list", lua.LGoFunc(hostsList))
	t.RawSetString("processes", lua.LGoFunc(hostsProcesses))
	t.Immutable = true
	return t
}

func memStats(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on memory").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.read on memory").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.read on memory").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.control on memory_limit").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "memory limit value required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	limit := l.CheckInt64(1)
	if limit < 0 {
		if limit == -1 {
			limit = math.MaxInt64
		} else {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "limit must be non-negative or -1").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.read on memory_limit").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.gc on gc").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.gc on gc_percent").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "percent value required").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.read on gc_percent").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.read on goroutines").WithKind(lua.Invalid).WithRetryable(false))
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
			l.Push(lua.NewLuaError(l, "permission denied: system.control on gomaxprocs").WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		n := l.CheckInt(1)
		if n <= 0 {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "GOMAXPROCS value must be positive").WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		prev := goruntime.GOMAXPROCS(n)
		l.Push(lua.LNumber(prev))
	} else {
		if !security.IsAllowed(l.Context(), "system.read", "gomaxprocs", nil) {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "permission denied: system.read on gomaxprocs").WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "permission denied: system.read on cpu").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	l.Push(lua.LNumber(goruntime.NumCPU()))
	l.Push(lua.LNil)
	return 2
}

func pid(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "pid", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on pid").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	l.Push(lua.LNumber(os.Getpid()))
	l.Push(lua.LNil)
	return 2
}

func cwd(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "cwd", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on cwd").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	dir, err := os.Getwd()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get working directory").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(dir))
	l.Push(lua.LNil)
	return 2
}

func hostname(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "hostname", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on hostname").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	name, err := os.Hostname()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get hostname").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(name))
	l.Push(lua.LNil)
	return 2
}

func exit(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.exit", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.exit").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	code := 0
	if l.GetTop() > 0 {
		code = l.CheckInt(1)
	}

	supervisor.TriggerShutdown(l.Context(), code)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func modules(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "modules", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on modules").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	cm := luaapi.GetCodeManager(l.Context())
	if cm == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "code manager not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	mods := cm.GetModules()
	result := l.CreateTable(len(mods), 0)

	for i, mod := range mods {
		modTable := l.CreateTable(0, 3)
		modTable.RawSetString("name", lua.LString(mod.Name))
		modTable.RawSetString("description", lua.LString(mod.Description))

		classTable := l.CreateTable(len(mod.Class), 0)
		for j, class := range mod.Class {
			classTable.RawSetInt(j+1, lua.LString(class))
		}
		modTable.RawSetString("class", classTable)

		result.RawSetInt(i+1, modTable)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
