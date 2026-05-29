<!-- SPDX-License-Identifier: MPL-2.0 -->

# process

Process management, spawning, messaging, and lifecycle events. Process, nondeterministic.

Global. No require needed.

```lua
process.spawn(...)  -- direct access
```

## Constants

```lua
process.event.CANCEL     -- "pid.cancel"
process.event.EXIT       -- "pid.exit"
process.event.LINK_DOWN  -- "pid.link.down"
```

## Dependencies

### channel (from engine)

Used by `process.inbox()`, `process.events()`, `process.listen()` for receiving messages.

| Method | Signature | Returns |
|--------|-----------|---------|
| receive | () | value, ok: boolean |
| close | () | - |
| case_receive | () | case |
| case_send | (value: any) | case |

See: `runtime/lua/engine/spec.md`

## Functions

### id() -> string, error

Returns the frame ID (call chain identifier) for the current process.

**Returns:** `string` - frame ID, or `nil, error` on failure

**Errors (strings):**
- `"no context found"` - no Lua context
- `"FrameContext not found"` - no frame context
- `"call ID not found in context"` - missing call ID

### pid() -> string, error

Returns the process ID (PID) for the current process.

**Returns:** `string` - PID string (e.g., `"{host|process|1234}"`), or `nil, error` on failure

**Errors (strings):**
- `"no context found"` - no Lua context

### send(destination: string, topic: string, ...) -> boolean, error

Sends message(s) to a process. The destination may be a raw PID string,
a globally-registered name, an eventually-registered name, or a locally
registered name — resolution and (for global names) ownership-fence
attachment + validation happen transparently inside the runtime. App
code does not need to call `process.registry.debug.lookup_with_fence`
or `process.registry.debug.validate_fence` to send safely.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name (raw / global / eventual / local) |
| topic | string | yes | - | Topic name (cannot start with `@`) |
| ... | any | no | - | Payload values to send |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"send requires at least destination and topic arguments"`
- `"cannot send to @ topics"` - reserved topic prefix
- `"no router found"`
- `"could not resolve: <name>"` - name not registered
- `"not allowed to send to: <pid>"` - permission denied
- `"stale fence"` - the receiving node observed a re-registration between resolution and delivery; safe to retry

### spawn(id: string, host: string, ...) -> string, error

Spawns a new process.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Process source ID (e.g., `"app.workers:my_worker"`) |
| host | string | yes | - | Host ID (e.g., `"app:processes"`) |
| ... | any | no | - | Arguments passed to spawned process |

**Returns:** `string` - PID of spawned process, or `nil, error` on failure

**Errors (strings):**
- `"spawn requires at least id and host arguments"`
- `"not allowed to spawn process: <id>"` - permission denied
- `"not allowed to spawn on host: <host>"` - permission denied
- Various spawn errors from process manager

### spawn_monitored(id: string, host: string, ...) -> string, error

Spawns a new process and automatically monitors it. Parent receives EXIT events.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Process source ID |
| host | string | yes | - | Host ID |
| ... | any | no | - | Arguments passed to spawned process |

**Returns:** `string` - PID of spawned process, or `nil, error` on failure

**Errors (strings):**
- `"spawn_monitored requires at least id and host arguments"`
- `"not allowed to spawn process: <id>"` - permission denied
- `"not allowed to spawn monitored process: <id>"` - permission denied

### spawn_linked(id: string, host: string, ...) -> string, error

Spawns a new process with bidirectional link. If either process exits abnormally, the other receives LINK_DOWN event.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Process source ID |
| host | string | yes | - | Host ID |
| ... | any | no | - | Arguments passed to spawned process |

**Returns:** `string` - PID of spawned process, or `nil, error` on failure

**Errors (strings):**
- `"spawn_linked requires at least id and host arguments"`
- `"not allowed to spawn process: <id>"` - permission denied
- `"not allowed to spawn linked process: <id>"` - permission denied

### spawn_linked_monitored(id: string, host: string, ...) -> string, error

Spawns a new process with both linking and monitoring.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Process source ID |
| host | string | yes | - | Host ID |
| ... | any | no | - | Arguments passed to spawned process |

**Returns:** `string` - PID of spawned process, or `nil, error` on failure

**Errors (strings):**
- `"spawn_linked_monitored requires at least id and host arguments"`
- Permission errors (see spawn, spawn_monitored, spawn_linked)

### terminate(destination: string) -> boolean, error

Forcefully terminates a process.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"not allowed to terminate: <pid>"` - permission denied
- `"could not resolve: <name>"` - name not registered

### cancel(destination: string, deadline?: integer|string) -> boolean, error

Requests graceful cancellation of a process. Sends CANCEL event to target.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name |
| deadline | integer\|string | no | 0 | Deadline: ms (integer) or Go duration (e.g., `"5s"`) |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"cancel requires at least destination argument"`
- `"invalid duration format: <err>"` - bad duration string
- `"deadline must be either a duration string or milliseconds number"`
- `"not allowed to cancel: <pid>"` - permission denied

### monitor(destination: string) -> boolean, error

Starts monitoring a process. Parent will receive EXIT events when monitored process exits.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"monitor requires a destination argument"`
- `"not allowed to monitor: <pid>"` - permission denied

### unmonitor(destination: string) -> boolean, error

Stops monitoring a process.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"unmonitor requires a destination argument"`
- `"not allowed to unmonitor: <pid>"` - permission denied

### link(destination: string) -> boolean, error

Creates bidirectional link with another process. LINK_DOWN events are sent when linked process exits abnormally.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"link requires a destination argument"`
- `"not allowed to link: <pid>"` - permission denied

**Notes:**
- LINK_DOWN is only received if `trap_links = true` in process options
- Normal exit does not trigger LINK_DOWN

### unlink(destination: string) -> boolean, error

Removes bidirectional link with another process.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| destination | string | yes | - | PID string or registered name |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (strings):**
- `"unlink requires a destination argument"`
- `"not allowed to unlink: <pid>"` - permission denied

### get_options() -> table

Returns current process options.

**Returns:** `table` with fields:

| Field | Type | Notes |
|-------|------|-------|
| trap_links | boolean | Whether LINK_DOWN events are delivered |

### set_options(options: table) -> boolean, error

Sets process options.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | yes | - | Options table |

**options fields:**

| Field | Type | Notes |
|-------|------|-------|
| trap_links | boolean | Enable/disable LINK_DOWN event delivery |

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (strings):**
- `"options parameter must be a table"`
- `"no process context"`
- `"trap_links must be a boolean"`
- `"option <name> is not supported"`

### inbox() -> channel

Returns channel for receiving inbox messages. Messages are wrapped as Message objects.

**Returns:** `channel` - yields until subscribed

**Yields:** until subscription established

### events() -> channel

Returns channel for receiving lifecycle events (CANCEL, EXIT, LINK_DOWN).

**Returns:** `channel` - yields until subscribed

**Yields:** until subscription established

**Event structure (received as table):**

| Field | Type | Notes |
|-------|------|-------|
| kind | string | Event type (`process.event.CANCEL`, `EXIT`, `LINK_DOWN`) |
| from | string | Source PID |
| result | table? | For EXIT: `{value: any}` or `{error: string}` |
| deadline | string? | For CANCEL: deadline timestamp |

### listen(topic: string, options?: table) -> channel

Subscribes to a custom topic.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| topic | string | yes | - | Topic name (cannot start with `@`) |
| options | table | no | nil | Subscription options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| message | boolean | false | If true, receive Message objects; if false, receive raw payloads |

**Returns:** `channel` - yields until subscribed

**Yields:** until subscription established

**Errors (strings):**
- `"topic cannot be empty"`
- `"cannot listen to @ topics"` - reserved prefix

**Notes:**
- Default: receives raw Lua values (tables, strings, etc.)
- With `{message = true}`: receives Message objects with `:topic()`, `:payload()`, `:from()` methods

### unlisten(channel: channel) -> nil

Unsubscribes from a topic.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| channel | channel | yes | - | Channel returned by `listen()` |

**Yields:** until unsubscription complete

### with_context(values: table) -> Spawner

Creates a Spawner with custom context values for child processes.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| values | table | yes | - | Key-value pairs to pass to child context |

**Returns:** `Spawner` object

### with_options(options: table) -> Spawner

Creates a Spawner with custom spawn options for child processes.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | yes | - | Key-value pairs mapped into `process.Start.Options` |

**Returns:** `Spawner` object

**Notes:**
- Options are opaque in this module and are forwarded unchanged to the host/runtime.
- Host implementations may interpret specific option keys (for example lifecycle, routing, or transport options).
- Validation of host-specific keys happens at the target host layer, not in `process` Lua module.

## process.registry

Subtable for process name registration.

### Scopes

`process.registry` exposes four explicit scopes, in increasing strictness:

| Constant                       | Wire | Backing store              | Semantics                                                                                                   |
|--------------------------------|------|----------------------------|-------------------------------------------------------------------------------------------------------------|
| `process.registry.LOCAL`       | 0    | per-node PID registry      | Visible only on the registering node. Default.                                                              |
| `process.registry.EVENTUAL`    | 2    | gossip / CRDT (eventualreg) | Cluster-wide, eventually consistent. No fence; converges after partition heal. Sized for ~1M presence names. |
| `process.registry.CONSISTENT`  | 1    | Raft (globalreg)            | Cluster-wide linearizable owner with fence token. Late ok. Scales to ~1M user-facing names.                 |
| `process.registry.STRONG`      | 3    | Raft + all-live-node ack    | Cluster-wide linearizable owner; activation requires every live node in the membership snapshot to ack the committed epoch within a deadline. No late compensation: a missing ack expires the registration. Reserved for the small set of root/control-plane names (<10k).        |

### process.registry.register(name: string, pid?: string, scope?: number) -> boolean, error

Registers a name, optionally pointing at a foreign PID and/or at a wider scope.

| Param | Type   | Required | Default | Notes                                                                                                |
|-------|--------|----------|---------|------------------------------------------------------------------------------------------------------|
| name  | string | yes      | —       | Name to register.                                                                                    |
| pid   | string | no       | self    | Target PID. When omitted (or `nil`), defaults to the caller's PID.                                   |
| scope | number | no       | LOCAL   | One of `process.registry.LOCAL` / `EVENTUAL` / `CONSISTENT` / `STRONG`.                              |

**Returns:** `true` on success, or `nil, error` on failure.

**Examples:**
```lua
process.registry.register("svc")                                       -- LOCAL, self
process.registry.register("svc", foreign_pid)                          -- LOCAL, foreign PID
process.registry.register("svc", nil, process.registry.STRONG)         -- STRONG, self
process.registry.register("svc", foreign_pid, process.registry.STRONG) -- STRONG, foreign PID
```

**Authorization (two axes):**
- **Per-scope name capability** — `process.registry.register.{local|eventual|consistent|strong}` on the *name* being registered.
- **Foreign-PID capability** — when `pid != self`, also requires `process.registry.foreign` on the *target PID*. Owner registering own PID does not need this. Default policy should deny foreign; operators grant it for supervisors, hot-upgrade flows, etc.

`STRONG` registration blocks the calling process until every live node in
the membership snapshot has acked the committed epoch, or until the
deadline elapses (default 10 s). On timeout, the runtime releases the
reservation and the call returns an error whose `MissingAcks` list names
the offending nodes.

**Errors (kinds):**
- `PermissionDenied` — capability gate failed (scope or foreign-pid axis).
- `AlreadyExists` — name taken by another process.
- `Invalid` — `scope` was not a number, or `pid` was not a parseable PID string.
- `Internal` — registry not available, raft not ready, or transport error.
- `StrongRegistrationTimeoutError` / `StrongConflictError` — `STRONG` specifically (timeout or terminal NACK).

### process.registry.lookup(name: string) -> string, error

Looks up PID by registered name.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Registered name |

**Returns:** `string` - PID string, or `nil, error` if not found

**Errors (strings):**
- `"name not registered"`

### process.registry.unregister(name: string) -> boolean

Removes a name registration.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Name to unregister |

**Returns:** `true` if name was registered and removed, `false` if not registered

**Errors (strings):**
- `"not allowed to unregister name: <name>"` - permission denied

## process.registry.debug

> Diagnostic / low-level only. **Prefer `process.send(name, ...)` for normal
> sends** — fences are attached and validated automatically by the runtime.
> These functions exist so operators can inspect the strongly consistent
> global registry directly; calling them from app code is a smell and emits a
> one-shot deprecation banner on stderr.

### process.registry.debug.lookup_with_fence(name: string) -> table, error

Looks up a globally registered name and returns both the PID and the
current fence (Raft log index of the registration). Used by chaos
probes and admin tools; **not** required for normal sends.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Globally registered name |

**Returns:** table `{ pid = "<pid-string>", fence_token = <number> }`, or `nil, error` if not found.

**Errors (strings):**
- `"name not registered"`
- `"global registry not available"`

### process.registry.debug.validate_fence(name: string, token: integer) -> boolean, error

Verifies that the supplied fence token still matches the current
registration of `name`. Used by chaos probes and admin tools.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Globally registered name |
| token | integer | yes | - | Fence token previously obtained from `debug.lookup_with_fence` |

**Returns:** `true` if the token is still current; `false, error` if the
registration changed or the name is no longer registered.

**Errors (strings):**
- `"stale fence"` - registration superseded since the token was issued
- `"global registry not available"`

### Deprecated top-level aliases

`process.registry.lookup_with_fence` and `process.registry.validate_fence`
are still callable for compatibility but emit a one-shot deprecation
warning on stderr. They will be removed in a future cycle — point
callers at `process.send(name, ...)` for normal use and
`process.registry.debug.*` for diagnostics.

## Types

### Message

Returned by `process.inbox()` receive or `process.listen()` with `{message = true}`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| topic | () | string | Topic the message was sent to |
| payload | () | any | Message payload(s) - single value or table of values |
| from | () | string? | Sender PID, or nil if unknown |

### Spawner

Returned by `process.with_context()` or `process.with_options()`. Used to spawn processes with custom context/options.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| with_context | (values: table) | Spawner | Add more context values (chainable) |
| with_options | (options: table) | Spawner | Merge spawn options (chainable) |
| with_actor | (actor: Actor) | Spawner | Set security actor |
| with_scope | (scope: Scope) | Spawner | Set security scope |
| spawn | (id: string, host: string, ...) | string, error | Spawn with context |
| spawn_monitored | (id: string, host: string, ...) | string, error | Spawn monitored with context |
| spawn_linked | (id: string, host: string, ...) | string, error | Spawn linked with context |
| spawn_linked_monitored | (id: string, host: string, ...) | string, error | Spawn linked+monitored with context |

## Example

```lua
-- Basic spawn and messaging
local child_pid, err = process.spawn_monitored("app.workers:echo", "app:processes", "hello")
if err then error(err) end

local events_ch = process.events()

-- Send message to child
process.send(child_pid, "request", {id = 1, data = "test"})

-- Wait for child to exit
local event = events_ch:receive()
if event.kind == process.event.EXIT then
    print("Child exited, from:", event.from)
end

-- Listen to custom topic
local ch = process.listen("responses")
local timeout = time.after("5s")

local result = channel.select {
    ch:case_receive(),
    timeout:case_receive()
}

if result.channel == ch then
    local data = result.value  -- raw Lua table/value
    print("Got response:", data.id)
end

-- Registry usage
process.registry.register("my_service")
local pid = process.registry.lookup("my_service")
process.send("my_service", "inbox", "hello")  -- send by name

-- Context passing to child
local spawner = process.with_context({
    request_id = "req-123",
    user_id = 42
})
local worker_pid = spawner:spawn_monitored("app.workers:handler", "app:processes")

-- Spawn options (opaque passthrough to host)
local configured_spawner = process
    .with_options({["custom.policy"] = "strict", ["custom.timeout_ms"] = 2500})
    :with_name("orders:update")
local worker_pid = configured_spawner:spawn_monitored("app.workers:update_handler", "app:processes")

-- Linked processes with trap_links
process.set_options({trap_links = true})
local linked_pid = process.spawn_linked("app.workers:partner", "app:processes")

local event = events_ch:receive()
if event.kind == process.event.LINK_DOWN then
    print("Partner died:", event.from)
end
```
