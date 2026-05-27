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

## system.node

Local node identity, address, and Raft role of THIS node. Read-only and
local-only — use `system.cluster.*` to enumerate or query other nodes.

### id() → string, error

Returns the local node identifier.

**Returns:** `string, error` - local NodeID or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| relay node unavailable | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `node`.

### addr() → string, error

Returns the local node network address as advertised through cluster membership.

**Returns:** `string, error` - local address or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| address unavailable | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `node`.

### role() → string, error

Returns the local Raft role: `"leader"`, `"voter"`, `"standby"` (non-voting learner), or `"non-member"`. When the Raft service is not wired into the context, returns `"non-member"`.

**Returns:** `string, error` - role or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `node`.

## system.cluster

Cluster-scoped read-only introspection. Owns cluster enumeration.

### members() → table[], error

Full cluster membership snapshot. Local node sorts first; remaining nodes by ID.

**Node info table fields:**

| Field | Type | Notes |
|-------|------|-------|
| id | string | NodeID |
| is_local | boolean | True if this entry is the local node |
| addr | string | Optional advertised address |
| meta | table | Optional string-keyed metadata |

**Returns:** `table[], error` - array of node info tables or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| no cluster information available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `cluster`.

### leader() → string, error

Current Raft leader NodeID. Returns the empty string when the leader is unknown or the Raft service is not wired.

**Returns:** `string, error` - leader NodeID (possibly empty) or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `cluster`.

### size() → integer, error

Number of alive members visible to the local node.

**Returns:** `integer, error` - count or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |

**Permissions:** Requires `system.read` on `cluster`.

## system.raft

Local Raft state introspection. All values are read from the local committed Raft state; no gossip, no mutation.

### is_leader() → boolean, error

True iff this node is currently the Raft leader.

**Returns:** `boolean, error` - leader flag or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| raft not available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `raft`.

### is_member() → boolean, error

True iff the local NodeID appears as voter or non-voter in the committed Raft configuration.

**Returns:** `boolean, error` - membership flag or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| raft not available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `raft`.

### role() → string, error

Local Raft role: `"leader"`, `"voter"`, `"standby"`, or `"non-member"`.

**Returns:** `string, error` - role or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| raft not available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `raft`.

### term() → integer, error

Current Raft term parsed from the local stats snapshot. Returns 0 when the stat is missing or unparsable.

**Returns:** `integer, error` - term or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| raft not available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `raft`.

### commit_index() → integer, error

Highest committed Raft log index on the local node.

**Returns:** `integer, error` - commit index or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| raft not available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `raft`.

### stats() → table, error

Full Raft stats snapshot as a table of string keys to string values (term, commit_index, last_log_index, state, etc.). Stricter permission than the other `system.raft` calls since the map is more revealing.

**Returns:** `table, error` - stats map or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.INVALID | no |
| raft not available | errors.INTERNAL | no |

**Permissions:** Requires `system.read` on `raft_stats`.

## system.lock

Distributed locks. A lock is a globally-unique name with a holder PID,
built on top of the STRONG global registry — no separate FSM, no
parallel gossip. The same all-live-node ack barrier that gates STRONG
registrations gates lock acquisition.

**Semantics:**
- Acquisition is **fail-fast**: no blocking waits in v1. Callers retry
  with their own backoff if a name is held.
- A lock is held by the registering process's PID. There is no
  "force release" — auto-release on holder process exit comes from the
  same FSM path that reaps any STRONG name on PID removal.
- Only the holder can release. A non-holder release returns `false`
  without touching the underlying registration.

### acquire(name: string) → boolean, error

Acquires the lock by registering `name` as a STRONG global name owned by
the calling process. Fail-fast: returns immediately on conflict.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Lock name (cluster-wide unique) |

**Returns:** `boolean, error` - `true` on success; `false` + conflict
error when already held; `nil` + structured error on other failure.

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.PERMISSION_DENIED | no |
| caller PID unavailable | errors.INTERNAL | no |
| global registry unavailable | errors.INTERNAL | no |
| already held / pending / rejected by peer | errors.ALREADY_EXISTS | no |

**Permissions:** Requires `system.lock` on the lock name (so policy can
restrict which names a caller may lock).

### release(name: string) → boolean, error

Releases the lock if the caller is the current holder.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Lock name |

**Returns:** `boolean, error` - `true` if the caller held the lock and
it was released; `false` if the lock is not held or is held by another
PID; `nil` + structured error on internal failure.

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.PERMISSION_DENIED | no |
| caller PID unavailable | errors.INTERNAL | no |
| global registry unavailable | errors.INTERNAL | no |

**Permissions:** Requires `system.lock` on the lock name.

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
