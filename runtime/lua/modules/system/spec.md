# Wippy Lua System Module Specification

## Overview

The `system` module grants Lua scripts access to the Go runtime and system information within Wippy applications. It's designed for diagnostics, performance monitoring, and managing runtime behavior like memory and garbage collection. All operations are subject to Wippy's security permission model.

## Module Interface

### Loading

```lua
local system = require("system")
```

### Return Values & Error Handling

All functions return two values:
1.  The primary result (e.g., a `number`, `string`, `table`, or `boolean`).
2.  An error message string if an error occurred or permission was denied, otherwise `nil`.

Example:
```lua
local count, err = system.num_goroutines()
if err then
    print("Error: " .. err)
    return
end
print("Goroutines: " .. count)
```

### Functions

All functions require appropriate `system.{action}` permissions (e.g., `system.read`, `system.gc`, `system.control`) for a specific resource (e.g., `memory`, `gc_percent`, `gomaxprocs`). The required permission is noted for each function.

---
**Memory & GC Statistics**
---

#### `system.mem_stats()`
Returns a table with detailed Go runtime memory statistics.
*Permission: `system.read` on resource `memory`.*
**Returns:** `table` (see example below), `error`
```lua
-- Example `mem_stats` table structure:
-- {
--   alloc = 8420344,        -- Current bytes allocated
--   total_alloc = 25648720, -- Cumulative bytes allocated
--   sys = 24234592,         -- Total memory from OS
--   heap_alloc = 8420344,   -- Heap bytes allocated
--   heap_sys = 16605184,    -- Heap memory from OS
--   heap_idle = 7274496,    -- Idle heap bytes
--   heap_in_use = 9330688,  -- In-use heap bytes
--   heap_released = 5242880,-- Heap bytes returned to OS
--   heap_objects = 42675,   -- Number of heap objects
--   stack_in_use = 2359296, -- Stack bytes in use
--   stack_sys = 2359296,    -- Stack memory from OS
--   mspan_in_use = 352128,  -- Mspan structure bytes
--   mspan_sys = 491520,     -- Mspan system bytes
--   num_gc = 12,            -- Completed GC cycles
--   next_gc = 16840688      -- Target heap size for next GC
-- }
```

#### `system.allocated()`
Returns current heap allocation in bytes (`mem_stats.alloc`).
*Permission: `system.read` on resource `memory`.*
**Returns:** `number`, `error`

#### `system.heap_objects()`
Returns the number of allocated heap objects (`mem_stats.heap_objects`).
*Permission: `system.read` on resource `memory`.*
**Returns:** `number`, `error`

---
**Garbage Collection Control**
---

#### `system.gc()`
Forces a Go runtime garbage collection cycle.
*Permission: `system.gc` on resource `gc`.*
**Returns:** `boolean` (true on success), `error`

#### `system.set_gc_percent(percent: number)`
Sets the GC target percentage. Lower values trigger GC more frequently. Passing `-1` attempts to disable GC (as per Go's `debug.SetGCPercent` behavior).
*Permission: `system.gc` on resource `gc_percent`.*
**Returns:** `number` (previous GC percentage, normalized to 100 if was -1), `error`

#### `system.get_gc_percent()`
Gets the current GC target percentage. Returns 100 if GC is effectively disabled or using the default.
*Permission: `system.read` on resource `gc_percent`.*
**Returns:** `number`, `error`

---
**Runtime & Process Control**
---

#### `system.set_memory_limit(limit: number)`
Sets the Go runtime's soft memory limit in bytes. Use `-1` for "unlimited" (Go's `math.MaxInt64`).
*Permission: `system.control` on resource `memory_limit`.*
**Returns:** `number` (previous limit; `math.MaxInt64` for previous "unlimited"), `error`

#### `system.get_memory_limit()`
Gets the current Go runtime's soft memory limit in bytes. `math.MaxInt64` indicates "unlimited".
*Permission: `system.read` on resource `memory_limit`.*
**Returns:** `number`, `error`

#### `system.go_max_procs([procs: number])`
Gets or sets GOMAXPROCS.
- With `procs` (positive integer): Sets GOMAXPROCS, returns previous value. *Permission: `system.control` on `gomaxprocs`.*
- Without `procs`: Gets current GOMAXPROCS. *Permission: `system.read` on `gomaxprocs`.*
  **Returns:** `number`, `error`

---
**Runtime & System Information**
---

#### `system.num_goroutines()`
Returns the current number of active goroutines.
*Permission: `system.read` on resource `goroutines`.*
**Returns:** `number`, `error`

#### `system.num_cpu()`
Returns the number of logical CPUs available.
*Permission: `system.read` on resource `cpu`.*
**Returns:** `number`, `error`

#### `system.hostname()`
Returns the system's hostname.
*Permission: `system.read` on resource `hostname`.*
**Returns:** `string`, `error`

#### `system.pid()`
Returns the current process ID (PID).
*Permission: `system.read` on resource `pid`.*
**Returns:** `number`, `error`

## Use Cases Summary

- **Memory Management:** Use `mem_stats`, `allocated`, `heap_objects` for monitoring. Use `set_memory_limit`, `get_memory_limit` to influence Go's memory targets.
- **GC Control:** Use `gc` to trigger collections, `set_gc_percent` and `get_gc_percent` to tune GC aggressiveness.
- **Runtime Tuning:** Use `go_max_procs` to manage CPU parallelism. `num_goroutines` and `num_cpu` provide context.
- **System Identification:** `hostname` and `pid` for logging and diagnostics.

## Thread Safety

Functions are safe for concurrent Lua coroutine use. Operations modifying global Go settings (GC percent, memory limit) are internally mutex-protected within this module.

## Best Practices Examples

1.  **Monitor Memory:**
    ```lua
    local stats, err = system.mem_stats()
    if not err then
        print(string.format("Alloc: %.2fMB, HeapObj: %d, Limit: %.0fMB",
            stats.alloc/1024/1024, stats.heap_objects, (system.get_memory_limit() or 0)/1024/1024))
    else print("Error fetching stats: " .. err) end
    ```

2.  **Tune GC Temporarily:**
    ```lua
    local original_gc, err_orig = system.get_gc_percent()
    if err_orig then print("Warn: " .. err_orig) else
        system.set_gc_percent(50) -- More aggressive
        -- ... intensive work ...
        system.set_gc_percent(original_gc) -- Restore
    end
    ```