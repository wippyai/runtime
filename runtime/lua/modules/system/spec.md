<!-- SPDX-License-Identifier: MPL-2.0 -->

# system

System memory, GC, runtime, and process information. Nondeterministic, process.

## Loading

```lua
local system = require("system")
```

## Functions

### exit(code?: integer) → boolean, error

Triggers system shutdown with exit code.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| code | integer | no | 0 | Exit code (0 = success) |

**Returns:** `boolean, error` - true on success or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.exit` permission.

### modules() → table[], error

Returns list of all loaded Lua modules with metadata.

**Returns:** `table[], error` - array of module info tables or nil + structured error

**Module info table fields:**

| Field | Type | Notes |
|-------|------|-------|
| name | string | Module name |
| description | string | Module description |
| class | string[] | Module classification tags |

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| code manager unavailable | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `modules`.

## system.memory

Memory statistics and control.

### stats() → table, error

Returns detailed memory statistics.

**Returns:** `table, error` - memory stats table or nil + structured error

**Stats table fields:**

| Field | Type | Notes |
|-------|------|-------|
| alloc | number | Bytes allocated and in use |
| total_alloc | number | Cumulative bytes allocated |
| sys | number | Bytes obtained from system |
| heap_alloc | number | Bytes allocated on heap |
| heap_sys | number | Bytes obtained for heap from system |
| heap_idle | number | Bytes in idle spans |
| heap_in_use | number | Bytes in non-idle spans |
| heap_released | number | Bytes released to OS |
| heap_objects | number | Number of allocated heap objects |
| stack_in_use | number | Bytes used by stack allocator |
| stack_sys | number | Bytes obtained for stack from system |
| mspan_in_use | number | Bytes of mspan structures in use |
| mspan_sys | number | Bytes obtained for mspan from system |
| num_gc | number | Number of completed GC cycles |
| next_gc | number | Target heap size for next GC |

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `memory`.

### allocated() → number, error

Returns current bytes allocated and in use.

**Returns:** `number, error` - bytes allocated or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `memory`.

### heap_objects() → number, error

Returns number of allocated heap objects.

**Returns:** `number, error` - object count or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `memory`.

### set_limit(limit: integer) → number, error

Sets memory limit and returns previous limit.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| limit | integer | yes | - | Memory limit in bytes, -1 for unlimited |

**Returns:** `number, error` - previous limit in bytes or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| missing limit argument | errors.INVALID | no |
| limit < -1 | errors.INVALID | no |

**Permissions:** Requires `system.control` on `memory_limit`.

### get_limit() → number, error

Returns current memory limit.

**Returns:** `number, error` - limit in bytes or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `memory_limit`.

## system.gc

Garbage collector control.

### collect() → boolean, error

Forces garbage collection.

**Returns:** `boolean, error` - true on success or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.gc` on `gc`.

### set_percent(percent: integer) → number, error

Sets GC target percentage and returns previous value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| percent | integer | yes | - | GC target percentage (100 = GC when heap doubles) |

**Returns:** `number, error` - previous percentage or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| missing percent argument | errors.INVALID | no |

**Permissions:** Requires `system.gc` on `gc_percent`.

**Notes:** If previous value was negative, returns 100 (default).

### get_percent() → number, error

Returns current GC target percentage.

**Returns:** `number, error` - percentage or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `gc_percent`.

**Notes:** If value is negative, returns 100 (default).

## system.runtime

Go runtime information and control.

### goroutines() → number, error

Returns number of active goroutines.

**Returns:** `number, error` - goroutine count or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `goroutines`.

### max_procs(n?: integer) → number, error

Gets or sets GOMAXPROCS value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| n | integer | no | nil | If provided, sets GOMAXPROCS to n (must be > 0) |

**Returns:** `number, error` - previous/current value or nil + structured error

**Behavior:**
- With argument: sets GOMAXPROCS and returns previous value (requires `system.control` on `gomaxprocs`)
- Without argument: returns current GOMAXPROCS (requires `system.read` on `gomaxprocs`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| n <= 0 | errors.INVALID | no |

### cpu_count() → number, error

Returns number of logical CPUs.

**Returns:** `number, error` - CPU count or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `cpu`.

## system.process

Process information.

### pid() → number, error

Returns current process ID.

**Returns:** `number, error` - process ID or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `pid`.

### hostname() → string, error

Returns system hostname.

**Returns:** `string, error` - hostname or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| OS error getting hostname | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `hostname`.

## system.supervisor

Service supervisor state queries.

### state(service_id: string) → table, error

Returns state for specific service.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| service_id | string | yes | - | Service ID (e.g., "namespace:service") |

**Returns:** `table, error` - service state table or nil + structured error

**State table fields:**

| Field | Type | Notes |
|-------|------|-------|
| id | string | Service ID |
| status | string | Current status |
| desired | string | Desired status |
| retry_count | number | Number of retries |
| last_update | number | Last update timestamp (nanoseconds) |
| started_at | number | Start timestamp (nanoseconds) |
| details | string | Optional details (formatted) |

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| service_id empty | errors.INVALID | no |
| service info unavailable | errors.INTERNAL | no |
| get state error | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `supervisor`.

### states() → table[], error

Returns states for all services.

**Returns:** `table[], error` - array of service state tables or nil + structured error

**State table format:** Same as `state()` function.

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| service info unavailable | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `supervisor`.

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = system.memory.stats()
if err then
    if err:kind() == errors.INVALID then
        -- permission denied or invalid argument
    elseif err:kind() == errors.INTERNAL then
        -- internal system error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local system = require("system")

-- Memory stats
local stats, err = system.memory.stats()
if err then error(err) end
print("Allocated:", stats.alloc)
print("Heap objects:", stats.heap_objects)
print("GC cycles:", stats.num_gc)

-- Quick memory check
local alloc, err = system.memory.allocated()
if err then error(err) end
print("Current allocation:", alloc)

-- GC control
local ok, err = system.gc.collect()
if err then error(err) end

local old_pct, err = system.gc.set_percent(200)
if err then error(err) end
print("Previous GC percent:", old_pct)

-- Runtime info
local goroutines, err = system.runtime.goroutines()
if err then error(err) end
print("Active goroutines:", goroutines)

local cpus, err = system.runtime.cpu_count()
if err then error(err) end
print("CPUs:", cpus)

local procs, err = system.runtime.max_procs()
if err then error(err) end
print("GOMAXPROCS:", procs)

-- Process info
local pid, err = system.process.pid()
if err then error(err) end
print("PID:", pid)

local hostname, err = system.process.hostname()
if err then error(err) end
print("Hostname:", hostname)

-- List modules
local mods, err = system.modules()
if err then error(err) end
for i, mod in ipairs(mods) do
    print(mod.name, "-", mod.description)
end
```
