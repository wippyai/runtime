// Package system provides a Lua module for accessing Go runtime and system information.
// It allows Lua scripts to inspect memory usage, control garbage collection,
// manage GOMAXPROCS, view CPU and goroutine counts, and retrieve system details
// like hostname and process ID.
//
// The module is designed with security in mind, requiring specific permissions
// for its operations, checked via the security.IsAllowed function.
// Access to sensitive settings like GC percentage and memory limits is protected
// by a mutex to ensure safe concurrent access from Lua via this module.
package system

import (
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"sync"

	systemapi "github.com/wippyai/runtime/api/system"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the system Lua module.
// It holds a mutex to protect concurrent access to global runtime settings
// like GC percentage and memory limit when modified through this module.
type Module struct {
	mu sync.Mutex // Mutex to protect access to GC percent and memory limit
}

// NewSystemModule creates and returns a new instance of the System Module.
func NewSystemModule() *Module {
	return &Module{} // mu is initialized to its zero value, which is usable
}

// Name returns the module's unique name: "system".
func (m *Module) Name() string {
	return "system"
}

// Loader is the function registered with gopher-lua to load the "system" module.
// It creates a Lua table populated with the module's functions.
func (m *Module) Loader(l *lua.LState) int {
	// Create a module table with exact pre-allocated size for its functions.
	mod := l.CreateTable(0, 14) // 14 exported functions

	// Register functions using RawSetString for potentially better performance.
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
	mod.RawSetString("set_memory_limit", l.NewFunction(m.setMemoryLimit))
	mod.RawSetString("get_memory_limit", l.NewFunction(m.getMemoryLimit))
	mod.RawSetString("exit", l.NewFunction(m.exit))

	l.Push(mod)
	return 1 // Number of values returned to Lua (the module table)
}

// memStats is a Lua-callable function that returns detailed Go runtime memory statistics.
// It pushes a table containing fields from runtime.MemStats and a nil error,
// or nil and an error string if not permitted or an issue occurs.
// Requires "system.read" permission for the "memory" resource.
func (*Module) memStats(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.RaiseError("permission denied: system.read on memory resource is required for mem_stats")
		return 0
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	statsTable := l.CreateTable(0, 15) // Pre-allocate for the 15 fields exposed
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
	return 2         // Return table and nil error
}

// allocated is a Lua-callable function that returns the number of bytes currently
// allocated by the Go runtime and not yet freed (runtime.MemStats.Alloc).
// Requires "system.read" permission for the "memory" resource.
func (*Module) allocated(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.RaiseError("permission denied: system.read on memory resource is required for allocated")
		return 0
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	l.Push(lua.LNumber(memStats.Alloc))
	l.Push(lua.LNil) // No error
	return 2
}

// heapObjects is a Lua-callable function that returns the number of allocated
// heap objects in the Go runtime (runtime.MemStats.HeapObjects).
// Requires "system.read" permission for the "memory" resource.
func (*Module) heapObjects(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory", nil) {
		l.RaiseError("permission denied: system.read on memory resource is required for heap_objects")
		return 0
	}
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	l.Push(lua.LNumber(memStats.HeapObjects))
	l.Push(lua.LNil) // No error
	return 2
}

// gc is a Lua-callable function that forces a garbage collection cycle using runtime.GC().
// Returns true and nil error on success.
// Requires "system.gc" permission for the "gc" resource.
func (*Module) gc(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.gc", "gc", nil) {
		l.RaiseError("permission denied: system.gc on gc resource is required to trigger garbage collection")
		return 0
	}

	runtime.GC()

	l.Push(lua.LBool(true))
	l.Push(lua.LNil) // No error
	return 2
}

// setGCPercent is a Lua-callable function that sets the Go runtime's garbage
// collection target percentage using debug.SetGCPercent.
// It takes one integer argument: the new GC percentage.
// It returns the previous GC percentage and a nil error.
// Requires "system.gc" permission for the "gc_percent" resource.
// Access to this global setting is protected by a mutex.
func (m *Module) setGCPercent(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.gc", "gc_percent", nil) {
		l.RaiseError("permission denied: system.gc on gc_percent resource is required to set GC percentage")
		return 0
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newSystemValidationError(l, "percent", "value required for set_gc_percent"))
		return 2
	}
	percent := l.CheckInt(1)

	m.mu.Lock()
	old := debug.SetGCPercent(percent)
	m.mu.Unlock()

	// Normalize the returned previous value if it was the default -1
	if old < 0 {
		old = 100
	}

	l.Push(lua.LNumber(old))
	l.Push(lua.LNil) // No error
	return 2
}

// getGCPercent is a Lua-callable function that returns the current Go runtime's
// garbage collection target percentage.
// It ensures the GC percentage is restored after querying to avoid side effects.
// If the actual percentage is -1 (GC disabled or GOGC default), it's normalized to 100.
// Requires "system.read" permission for the "gc_percent" resource.
// Access to this global setting is protected by a mutex.
func (m *Module) getGCPercent(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "gc_percent", nil) {
		l.RaiseError("permission denied: system.read on gc_percent resource is required to get GC percentage")
		return 0
	}

	m.mu.Lock()
	// debug.SetGCPercent(-1) sets GC off and returns the previous value.
	originalPercent := debug.SetGCPercent(-1)
	// Restore the original GC percentage immediately.
	debug.SetGCPercent(originalPercent)
	m.mu.Unlock()

	luaPercentToReturn := originalPercent
	// In Go, GOGC can be -1. Normalize to 100 for Lua if it was off/default.
	if luaPercentToReturn < 0 {
		luaPercentToReturn = 100
	}

	l.Push(lua.LNumber(luaPercentToReturn))
	l.Push(lua.LNil) // No error
	return 2
}

// numGoroutines is a Lua-callable function that returns the current number
// of active goroutines in the Go runtime using runtime.NumGoroutine().
// Requires "system.read" permission for the "goroutines" resource.
func (*Module) numGoroutines(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "goroutines", nil) {
		l.RaiseError("permission denied: system.read on goroutines resource is required for num_goroutines")
		return 0
	}

	count := runtime.NumGoroutine()

	l.Push(lua.LNumber(count))
	l.Push(lua.LNil) // No error
	return 2
}

// goMaxProcs is a Lua-callable function that gets or sets the maximum number of
// CPUs that the Go runtime can use simultaneously (GOMAXPROCS).
//   - If called with an integer argument, it sets GOMAXPROCS to that value and
//     returns the previous GOMAXPROCS value. Requires "system.control" permission
//     for the "gomaxprocs" resource.
//   - If called with no arguments, it returns the current GOMAXPROCS value.
//     Requires "system.read" permission for the "gomaxprocs" resource.
func (*Module) goMaxProcs(l *lua.LState) int {
	if l.GetTop() > 0 { // Setter
		if !security.IsAllowed(l.Context(), "system.control", "gomaxprocs", nil) {
			l.RaiseError("permission denied: system.control on gomaxprocs resource is required to set GOMAXPROCS")
			return 0
		}
		newProcs := l.CheckInt(1)
		if newProcs <= 0 {
			l.Push(lua.LNil)
			l.Push(newSystemValidationError(l, "GOMAXPROCS", "value must be positive"))
			return 2
		}
		previousProcs := runtime.GOMAXPROCS(newProcs)
		l.Push(lua.LNumber(previousProcs))
	} else { // Getter
		if !security.IsAllowed(l.Context(), "system.read", "gomaxprocs", nil) {
			l.RaiseError("permission denied: system.read on gomaxprocs resource is required to get GOMAXPROCS")
			return 0
		}
		// runtime.GOMAXPROCS(0) returns the current value without changing it.
		currentProcs := runtime.GOMAXPROCS(0)
		l.Push(lua.LNumber(currentProcs))
	}

	l.Push(lua.LNil) // No error
	return 2
}

// numCPU is a Lua-callable function that returns the number of logical CPUs
// usable by the current process, as reported by runtime.NumCPU().
// Requires "system.read" permission for the "cpu" resource.
func (*Module) numCPU(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "cpu", nil) {
		l.RaiseError("permission denied: system.read on cpu resource is required for num_cpu")
		return 0
	}

	count := runtime.NumCPU()

	l.Push(lua.LNumber(count))
	l.Push(lua.LNil) // No error
	return 2
}

// hostname is a Lua-callable function that returns the host name reported by
// the kernel, using os.Hostname().
// Requires "system.read" permission for the "hostname" resource.
func (*Module) hostname(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "hostname", nil) {
		l.RaiseError("permission denied: system.read on hostname resource is required for hostname")
		return 0
	}

	name, err := os.Hostname()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newSystemIOError(l, err, "hostname"))
		return 2
	}

	l.Push(lua.LString(name))
	l.Push(lua.LNil) // No error
	return 2
}

// pid is a Lua-callable function that returns the process ID of the caller
// using os.Getpid().
// Requires "system.read" permission for the "pid" resource.
func (*Module) pid(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "pid", nil) {
		l.RaiseError("permission denied: system.read on pid resource is required for pid")
		return 0
	}

	pidNum := os.Getpid()

	l.Push(lua.LNumber(pidNum))
	l.Push(lua.LNil) // No error
	return 2
}

// setMemoryLimit is a Lua-callable function that sets the Go runtime's soft memory limit
// using debug.SetMemoryLimit.
// It takes one integer argument: the memory limit in bytes. A value of -1 means
// "unlimited" (effectively math.MaxInt64).
// It returns the previous memory limit in bytes and a nil error.
// Requires "system.control" permission for the "memory_limit" resource.
// Access to this global setting is protected by a mutex.
func (m *Module) setMemoryLimit(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.control", "memory_limit", nil) {
		l.RaiseError("permission denied: system.control on memory_limit resource is required to set memory limit")
		return 0
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(newSystemValidationError(l, "memory_limit", "value (bytes) required for set_memory_limit"))
		return 2
	}
	limit := l.CheckInt64(1)
	if limit < 0 {
		if limit == -1 { // User convention for "unlimited"
			limit = math.MaxInt64 // Go's representation of effectively no limit
		} else {
			l.Push(lua.LNil)
			l.Push(newSystemValidationError(l, "memory_limit", "must be non-negative, or -1 for unlimited"))
			return 2
		}
	}

	m.mu.Lock()
	previousLimit := debug.SetMemoryLimit(limit)
	m.mu.Unlock()

	l.Push(lua.LNumber(previousLimit))
	l.Push(lua.LNil) // No error
	return 2
}

// getMemoryLimit is a Lua-callable function that returns the current Go runtime's
// soft memory limit in bytes.
// It ensures the memory limit is restored after querying to avoid side effects.
// A returned value of math.MaxInt64 (a very large number) typically indicates
// an "unlimited" or default high limit.
// Requires "system.read" permission for the "memory_limit" resource.
// Access to this global setting is protected by a mutex.
func (m *Module) getMemoryLimit(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "memory_limit", nil) {
		l.RaiseError("permission denied: system.read on memory_limit resource is required to get memory limit")
		return 0
	}

	m.mu.Lock()
	// debug.SetMemoryLimit(-1) sets the limit to math.MaxInt64 and returns the *previous* actual limit.
	currentLimit := debug.SetMemoryLimit(-1)
	// Restore the actual current limit immediately.
	debug.SetMemoryLimit(currentLimit)
	m.mu.Unlock()

	l.Push(lua.LNumber(currentLimit))
	l.Push(lua.LNil) // No error
	return 2
}

// exit is a Lua-callable function that triggers graceful application shutdown
// with a specified exit code.
// It takes one optional integer argument: the exit code (defaults to 0).
// The function sets the exit code and sends a SIGTERM signal to trigger shutdown.
// Requires "system.exit" permission.
func (*Module) exit(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.exit", "", nil) {
		l.RaiseError("permission denied: system.exit required")
		return 0
	}

	code := 0
	if l.GetTop() > 0 {
		code = l.CheckInt(1)
	}

	systemapi.TriggerShutdown(l.Context(), code)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}
