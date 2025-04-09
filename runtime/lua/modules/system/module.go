package system

import (
	"os"
	"runtime"
	"runtime/debug"

	lua "github.com/yuin/gopher-lua"
)

// Module represents a system Lua module.
type Module struct{}

// NewSystemModule creates and returns a new instance of the System Module.
func NewSystemModule() *Module {
	return &Module{}
}

// Name returns the module's name.
func (m *Module) Name() string {
	return "system"
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	// Create a module table with exact pre-allocated size
	mod := l.CreateTable(0, 11) // Exactly 11 functions

	// Register functions using RawSetString for better performance
	mod.RawSetString("mem_stats", l.NewFunction(m.memStats))
	mod.RawSetString("allocated", l.NewFunction(m.allocated))
	mod.RawSetString("heap_objects", l.NewFunction(m.heapObjects))
	mod.RawSetString("gc", l.NewFunction(m.gc))
	mod.RawSetString("set_gc_percent", l.NewFunction(m.setGCPercent))
	mod.RawSetString("get_gc_percent", l.NewFunction(m.getGCPercent))
	mod.RawSetString("num_goroutines", l.NewFunction(m.numGoroutines))
	mod.RawSetString("go_max_procs", l.NewFunction(m.goMaxProcs))
	mod.RawSetString("num_cpu", l.NewFunction(m.numCPU))
	mod.RawSetString("hostname", l.NewFunction(m.hostname))
	mod.RawSetString("pid", l.NewFunction(m.pid))

	l.Push(mod)
	return 1
}

// memStats returns detailed memory statistics.
func (*Module) memStats(l *lua.LState) int {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Create a Lua table to hold memory stats
	statsTable := l.CreateTable(0, 15)
	statsTable.RawSetString("alloc", lua.LNumber(memStats.Alloc))
	statsTable.RawSetString("total_alloc", lua.LNumber(memStats.TotalAlloc))
	statsTable.RawSetString("sys", lua.LNumber(memStats.Sys))
	statsTable.RawSetString("heap_alloc", lua.LNumber(memStats.HeapAlloc))
	statsTable.RawSetString("heap_sys", lua.LNumber(memStats.HeapSys))
	statsTable.RawSetString("heap_idle", lua.LNumber(memStats.HeapIdle))
	statsTable.RawSetString("heap_in_use", lua.LNumber(memStats.HeapInuse))
	statsTable.RawSetString("heap_released", lua.LNumber(memStats.HeapReleased))
	statsTable.RawSetString("heap_objects", lua.LNumber(memStats.HeapObjects))
	statsTable.RawSetString("stack_in_use", lua.LNumber(memStats.StackInuse))
	statsTable.RawSetString("stack_sys", lua.LNumber(memStats.StackSys))
	statsTable.RawSetString("mspan_in_use", lua.LNumber(memStats.MSpanInuse))
	statsTable.RawSetString("mspan_sys", lua.LNumber(memStats.MSpanSys))
	statsTable.RawSetString("num_gc", lua.LNumber(memStats.NumGC))
	statsTable.RawSetString("next_gc", lua.LNumber(memStats.NextGC))

	l.Push(statsTable)
	l.Push(lua.LNil) // No error
	return 2
}

// allocated returns the number of bytes allocated and not yet freed.
func (*Module) allocated(l *lua.LState) int {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	l.Push(lua.LNumber(memStats.Alloc))
	l.Push(lua.LNil) // No error
	return 2
}

// heapObjects returns the number of allocated heap objects.
func (*Module) heapObjects(l *lua.LState) int {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	l.Push(lua.LNumber(memStats.HeapObjects))
	l.Push(lua.LNil) // No error
	return 2
}

// gc forces a garbage collection.
func (*Module) gc(l *lua.LState) int {
	runtime.GC()

	l.Push(lua.LBool(true))
	l.Push(lua.LNil) // No error
	return 2
}

// setGCPercent sets the garbage collection target percentage.
func (*Module) setGCPercent(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("percent value required"))
		return 2
	}

	percent := l.CheckInt(1)
	old := debug.SetGCPercent(percent)

	// Normalize the returned value for consistency
	if old < 0 {
		old = 100
	}

	l.Push(lua.LNumber(old))
	l.Push(lua.LNil) // No error
	return 2
}

// getGCPercent returns the current garbage collection target percentage.
func (*Module) getGCPercent(l *lua.LState) int {
	// SetGCPercent(-1) returns current value without changing it
	percent := debug.SetGCPercent(-1)

	// In Go, the GC percent can be -1 if not explicitly set
	// Convert to 100 which is the standard default in most cases
	if percent < 0 {
		percent = 100
	}

	l.Push(lua.LNumber(percent))
	l.Push(lua.LNil) // No error
	return 2
}

// numGoroutines returns the number of goroutines that currently exist.
func (*Module) numGoroutines(l *lua.LState) int {
	count := runtime.NumGoroutine()

	l.Push(lua.LNumber(count))
	l.Push(lua.LNil) // No error
	return 2
}

// goMaxProcs sets or gets the maximum number of CPUs that can be executing simultaneously.
func (*Module) goMaxProcs(l *lua.LState) int {
	var procs int

	if l.GetTop() > 0 {
		// Set GOMAXPROCS if argument provided
		newProcs := l.CheckInt(1)
		procs = runtime.GOMAXPROCS(newProcs)
	} else {
		// Get current GOMAXPROCS if no argument
		procs = runtime.GOMAXPROCS(-1) // -1 returns current value without changing it
	}

	l.Push(lua.LNumber(procs))
	l.Push(lua.LNil) // No error
	return 2
}

// numCPU returns the number of logical CPUs usable by the current process.
func (*Module) numCPU(l *lua.LState) int {
	count := runtime.NumCPU()

	l.Push(lua.LNumber(count))
	l.Push(lua.LNil) // No error
	return 2
}

// hostname returns the host name reported by the kernel.
func (*Module) hostname(l *lua.LState) int {
	name, err := os.Hostname()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(name))
	l.Push(lua.LNil) // No error
	return 2
}

// pid returns the process ID of the caller.
func (*Module) pid(l *lua.LState) int {
	pid := os.Getpid()

	l.Push(lua.LNumber(pid))
	l.Push(lua.LNil) // No error
	return 2
}
