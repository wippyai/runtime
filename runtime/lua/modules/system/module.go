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
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	systemapi "github.com/wippyai/runtime/api/system"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the system Lua module.
// It holds a mutex to protect concurrent access to global runtime settings
// like GC percentage and memory limit when modified through this module.
type Module struct {
	mu              sync.Mutex
	once            sync.Once
	moduleTable     *lua.LTable
	memoryTable     *lua.LTable
	gcTable         *lua.LTable
	runtimeTable    *lua.LTable
	processTable    *lua.LTable
	supervisorTable *lua.LTable
}

// NewSystemModule creates and returns a new instance of the System Module.
func NewSystemModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "system",
		Description: "Go runtime and system information",
		Class:       []string{luaapi.ClassSecurity, luaapi.ClassNondeterministic},
	}
}

// Loader is the function registered with gopher-lua to load the "system" module.
// It creates a Lua table populated with child tables and top-level functions.
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTables(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTables creates and initializes all module tables once
func (m *Module) initModuleTables(l *lua.LState) {
	// Create child tables
	m.memoryTable = m.initMemoryTable(l)
	m.gcTable = m.initGCTable(l)
	m.runtimeTable = m.initRuntimeTable(l)
	m.processTable = m.initProcessTable(l)
	m.supervisorTable = m.initSupervisorTable(l)

	// Create main module table with child tables and top-level functions
	mod := l.CreateTable(0, 7)
	mod.RawSetString("memory", m.memoryTable)
	mod.RawSetString("gc", m.gcTable)
	mod.RawSetString("runtime", m.runtimeTable)
	mod.RawSetString("process", m.processTable)
	mod.RawSetString("supervisor", m.supervisorTable)
	mod.RawSetString("exit", l.NewFunction(m.exit))
	mod.RawSetString("modules", l.NewFunction(m.modules))

	mod.Immutable = true
	m.moduleTable = mod
}

// initMemoryTable creates the memory child table
func (m *Module) initMemoryTable(l *lua.LState) *lua.LTable {
	t := l.CreateTable(0, 5)
	t.RawSetString("stats", l.NewFunction(m.memStats))
	t.RawSetString("allocated", l.NewFunction(m.allocated))
	t.RawSetString("heap_objects", l.NewFunction(m.heapObjects))
	t.RawSetString("set_limit", l.NewFunction(m.setMemoryLimit))
	t.RawSetString("get_limit", l.NewFunction(m.getMemoryLimit))
	t.Immutable = true
	return t
}

// initGCTable creates the gc child table
func (m *Module) initGCTable(l *lua.LState) *lua.LTable {
	t := l.CreateTable(0, 3)
	t.RawSetString("collect", l.NewFunction(m.gc))
	t.RawSetString("set_percent", l.NewFunction(m.setGCPercent))
	t.RawSetString("get_percent", l.NewFunction(m.getGCPercent))
	t.Immutable = true
	return t
}

// initRuntimeTable creates the runtime child table
func (m *Module) initRuntimeTable(l *lua.LState) *lua.LTable {
	t := l.CreateTable(0, 3)
	t.RawSetString("goroutines", l.NewFunction(m.numGoroutines))
	t.RawSetString("max_procs", l.NewFunction(m.goMaxProcs))
	t.RawSetString("cpu_count", l.NewFunction(m.numCPU))
	t.Immutable = true
	return t
}

// initProcessTable creates the process child table
func (m *Module) initProcessTable(l *lua.LState) *lua.LTable {
	t := l.CreateTable(0, 3)
	t.RawSetString("stats", l.NewFunction(m.processStats))
	t.RawSetString("pid", l.NewFunction(m.pid))
	t.RawSetString("hostname", l.NewFunction(m.hostname))
	t.Immutable = true
	return t
}

// initSupervisorTable creates the supervisor child table
func (m *Module) initSupervisorTable(l *lua.LState) *lua.LTable {
	t := l.CreateTable(0, 2)
	t.RawSetString("state", l.NewFunction(m.supervisorState))
	t.RawSetString("states", l.NewFunction(m.supervisorStates))
	t.Immutable = true
	return t
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

// processStats collects statistics from all process hosts.
// Returns a table containing stats from all hosts or nil and error.
// Requires "system.read" permission for "process_stats" resource.
func (*Module) processStats(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "process_stats", nil) {
		l.RaiseError("permission denied: system.read on process_stats required")
		return 0
	}

	hosts := process.GetHosts(l.Context())
	if hosts == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("process hosts not available"))
		return 2
	}

	snapshots, err := hosts.CollectAll(l.Context())
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Build Lua table manually for optimal serialization
	result := l.CreateTable(len(snapshots), 0)

	for i, snapshot := range snapshots {
		hostTable := l.CreateTable(0, 5)
		hostTable.RawSetString("host_id", lua.LString(snapshot.HostID))
		hostTable.RawSetString("timestamp", lua.LNumber(snapshot.Timestamp.UnixNano()))
		hostTable.RawSetString("enabled", lua.LBool(snapshot.Enabled))
		hostTable.RawSetString("sample_rate", lua.LNumber(snapshot.SampleRate))

		// Build processes array
		processesTable := l.CreateTable(len(snapshot.Processes), 0)
		for j, entry := range snapshot.Processes {
			entryTable := l.CreateTable(0, 8)
			entryTable.RawSetString("pid", lua.LString(entry.PID.String()))
			entryTable.RawSetString("id", lua.LString(entry.SourceID))
			entryTable.RawSetString("started_at", lua.LNumber(entry.StartedAt.UnixNano()))
			entryTable.RawSetString("step_count", lua.LNumber(entry.StepCount))
			entryTable.RawSetString("last_activity_at", lua.LNumber(entry.LastActivityAt.UnixNano()))

			if entry.Actor != "" {
				entryTable.RawSetString("actor", lua.LString(entry.Actor))
			}

			// Convert Options map to Lua table (inline for known types)
			if len(entry.Options) > 0 {
				optionsTable := l.CreateTable(0, len(entry.Options))
				for k, v := range entry.Options {
					switch val := v.(type) {
					case string:
						optionsTable.RawSetString(k, lua.LString(val))
					case bool:
						optionsTable.RawSetString(k, lua.LBool(val))
					default:
						optionsTable.RawSetString(k, lua.LString(fmt.Sprint(val)))
					}
				}
				entryTable.RawSetString("options", optionsTable)
			}

			// Convert Info map to Lua table (inline for known types)
			if len(entry.Info) > 0 {
				infoTable := l.CreateTable(0, len(entry.Info))
				for k, v := range entry.Info {
					switch val := v.(type) {
					case string:
						infoTable.RawSetString(k, lua.LString(val))
					case bool:
						infoTable.RawSetString(k, lua.LBool(val))
					case int64:
						infoTable.RawSetString(k, lua.LNumber(val))
					case float64:
						infoTable.RawSetString(k, lua.LNumber(val))
					default:
						infoTable.RawSetString(k, lua.LString(fmt.Sprint(val)))
					}
				}
				entryTable.RawSetString("info", infoTable)
			}

			processesTable.RawSetInt(j+1, entryTable)
		}
		hostTable.RawSetString("processes", processesTable)

		result.RawSetInt(i+1, hostTable)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// supervisorState returns the state of a specific supervised service.
// Requires "system.read" permission for the "supervisor" resource.
func (*Module) supervisorState(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "supervisor", nil) {
		l.RaiseError("permission denied: system.read on supervisor resource required")
		return 0
	}

	serviceIDStr := l.CheckString(1)
	if serviceIDStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("service ID required"))
		return 2
	}

	serviceInfo := systemapi.GetServiceInfo(l.Context())
	if serviceInfo == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("service info not available in context"))
		return 2
	}

	serviceID := registry.ParseID(serviceIDStr)
	state, err := serviceInfo.GetState(serviceID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	stateTable := l.CreateTable(0, 7)
	stateTable.RawSetString("id", lua.LString(state.ID.String()))
	stateTable.RawSetString("status", lua.LString(state.Status))
	stateTable.RawSetString("desired", lua.LString(state.Desired))
	stateTable.RawSetString("retry_count", lua.LNumber(state.RetryCount))
	stateTable.RawSetString("last_update", lua.LNumber(state.LastUpdate.UnixNano()))
	stateTable.RawSetString("started_at", lua.LNumber(state.StartedAt.UnixNano()))
	if state.Details != nil {
		stateTable.RawSetString("details", lua.LString(fmt.Sprintf("%v", state.Details)))
	}

	l.Push(stateTable)
	l.Push(lua.LNil)
	return 2
}

// supervisorStates returns the states of all supervised services.
// Requires "system.read" permission for the "supervisor" resource.
func (*Module) supervisorStates(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "supervisor", nil) {
		l.RaiseError("permission denied: system.read on supervisor resource required")
		return 0
	}

	serviceInfo := systemapi.GetServiceInfo(l.Context())
	if serviceInfo == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("service info not available in context"))
		return 2
	}

	allStates := serviceInfo.GetAllStates()
	result := l.CreateTable(len(allStates), 0)

	for i, state := range allStates {
		stateTable := l.CreateTable(0, 7)
		stateTable.RawSetString("id", lua.LString(state.ID.String()))
		stateTable.RawSetString("status", lua.LString(state.Status))
		stateTable.RawSetString("desired", lua.LString(state.Desired))
		stateTable.RawSetString("retry_count", lua.LNumber(state.RetryCount))
		stateTable.RawSetString("last_update", lua.LNumber(state.LastUpdate.UnixNano()))
		stateTable.RawSetString("started_at", lua.LNumber(state.StartedAt.UnixNano()))
		if state.Details != nil {
			stateTable.RawSetString("details", lua.LString(fmt.Sprintf("%v", state.Details)))
		}

		result.RawSetInt(i+1, stateTable)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// modules returns information about all registered Lua modules.
// Returns a table with module info including name, description, and classes.
// Requires "system.read" permission for the "modules" resource.
func (*Module) modules(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "modules", nil) {
		l.RaiseError("permission denied: system.read on modules resource required")
		return 0
	}

	cm := luaapi.GetCodeManager(l.Context())
	if cm == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("code manager not available"))
		return 2
	}

	modules := cm.GetModules()
	result := l.CreateTable(len(modules), 0)

	for i, mod := range modules {
		modTable := l.CreateTable(0, 3)
		modTable.RawSetString("name", lua.LString(mod.Name))
		modTable.RawSetString("description", lua.LString(mod.Description))

		// Build classes array
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
