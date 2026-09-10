# Lua agents and remote terminal surfaces

The SDK is sufficient for a V1 agent to observe and control a terminal on another
mesh node. The two nodes must already be members of the runtime mesh. Lua uses
process identities and mount references; it does not open a socket to a terminal
IP address.

A **node** is a runtime instance in the mesh. A process **host** is the scheduler
or execution host inside that runtime, not a machine IP. A PID identifies both,
for example `{node-b@agents|unique-process-id}`. Obtain PIDs from `process.pid()`,
spawn results, or your application's process discovery; do not fabricate them.

| Object/action | Meaning |
|---|---|
| `tty.surface()` | Output owned by the current process; presents composed rows. |
| `tty.viewport()` | A local broker that stores a producer's current terminal frame. |
| `view:grant()` | One-use producer binding, supplied through the child's `terminal` option. |
| `view:mount(pid, rights)` | One-use observer/controller reference for that exact process, including its node. |
| `tty.attach(ref)` | Consume a local or remote mount; the returned viewport API is the same. |
| `view:snapshot()` / `updates()` | Observe the cached current frame and revision notifications. |
| `view:send(event)` / `resize(w,h)` | Deliver input or resize under the mount's independent rights. |
| `view:close()` | Detach this view. Closing an issuing viewport also revokes its mounts. |
| `owner:revoke(ref)` | Revoke a specific issued mount. |

`grant()` and `mount()` serve different roles. An agent's observation mount
cannot present output, mint producer bindings, or delegate access to another
agent. Create a separate mount for each agent. Combine rights in one mount when
one agent needs them all, or use separate observation and control mounts.

## Connecting an agent on node A to a terminal on node B

1. Your application locates an authorized controller process on node B. An
   existing `process.send(remote_controller_pid, topic, payload)` can reach it.
2. That controller creates the viewport and starts a producer **on node B** with
   its producer grant. The producer signals readiness after `tty.start()` and,
   for a native command, after attaching the PTY session.
3. The controller creates a mount bound to the agent's actual PID on node A and
   sends the reference back with ordinary process messaging.
4. The agent calls `tty.attach(ref)`, subscribes to updates, and reads snapshots.
   It can send input and resize if granted those rights.

A process running on A does not cause `tty.viewport()` or `process.spawn()` to
execute on B merely by using B's IP as a host argument. Remote startup is an
application/controller action; the mount connects the resulting surface.

On the owner, after the producer is ready and the application has authorized
`agent_pid`:

```lua
local tty = require("tty")
local process = require("process")

-- view is the viewport whose grant was given to the local producer.
local ref, err = view:mount(agent_pid, {
    observe = true, input = true, resize = true,
})
if not ref then error(tostring(err)) end
local sent, send_err = process.send(agent_pid, "terminal.mount", {ref = ref})
if not sent then
    view:revoke(ref)
    error(tostring(send_err))
end
```

The owner needs `tty.mount` plus each delegated right on that viewport. Normal
process messaging, spawning, and executor permissions still apply. A privileged
controller must authorize requests before minting mounts; never turn an arbitrary
PID in an incoming payload into an automatic access grant.

On the agent, subscribe **before** requesting a surface so a fast reply cannot
arrive before the listener exists:

```lua
local tty = require("tty")
local process = require("process")

local replies = process.listen("terminal.mount", {message = true})
-- Request through your controller's application protocol here.
local message, open = replies:receive()
if not open then return end
-- controller_pid is the exact owner PID resolved by the application.
if message:from() ~= controller_pid then error("unexpected controller") end
local reply = message:payload():data()
local view, err = tty.attach(reply.ref)
if not view then error(tostring(err)) end
local updates, update_err = view:updates()
if not updates then error(tostring(update_err)) end
local snapshot, snapshot_err = view:snapshot()
if not snapshot then error(tostring(snapshot_err)) end

local ok, send_err = view:send({type = "paste", text = "echo hello"})
if not ok then error(tostring(send_err)) end
ok, send_err = view:send({
    type = "key", key = "enter", key_type = "enter", action = "press",
})
if not ok then error(tostring(send_err)) end

while true do
    local revision, alive = updates:receive()
    if not alive then break end
    local current, read_err = view:snapshot(snapshot.revision)
    if read_err then break end
    if current then
        snapshot = current
        -- Inspect snapshot.rows, or composite them into a local canvas.
    end
end
view:close()
```

This illustrates the mount flow, not a complete controller protocol. A controller
also needs request correlation, authorization, startup deadlines, and producer
exit handling. The executable harness in this directory verifies the real Lua,
PTY, mesh, and snapshot path independently of those application choices.

## Simulation, observation, and local controls

`tty.InputEvent` accepts synthetic key/mouse events with omitted `alt`, `ctrl`,
and `shift` flags; they default to false. Keys require `key`, `key_type`, and
`action`. Mouse coordinates are one-based cells relative to the remote viewport:

```lua
view:send({type="mouse", action="press", button="left", x=12, y=3})
view:send({type="mouse", action="release", button="left", x=12, y=3})
```

`send()` success means the owner broker accepted the event for the producer. It
is not proof that a shell command finished or that the application acted on a
click. Confirm completion through a unique output marker or an explicit
application acknowledgement. Snapshots contain styled terminal rows and cursor
state, not the application's internal model or a complete output history. Wait
on updates and use `snapshot(last_revision)` to avoid allocating a new Lua
screen table when the revision is unchanged.

Local and remote snapshots compose the same way: clip rows into canvas regions,
paint local controls outside those regions, then present through one Surface.
Translate mouse coordinates into the focused region. Forward remote events from
a separate coroutine with a bounded queue, so a slow request cannot block local
close controls. Report discrete-input overflow; coalesce only transient motion.

## Exit, restart, and reconnect

Close detaches a consumer; it does not inherently kill a producer. A `close`
event is a request the producer must handle. To terminate a process, use the
application's explicit stop operation or authorized process cancellation.

Observation, input, and resize permissions are independent. Input-only mounts
still receive owner-exit/revocation notifications. Closed views reject further
operations and clear their cached screens. Content already delivered to an
agent cannot be recalled.

Owner or agent restart invalidates that generation's mounts. Rediscover the new
PID and request fresh references. A transport interruption may recover an
in-flight request, with duplicate input suppressed, but a timed-out attachment
fails closed. Do not automatically replay an uncertain command after reattaching.
Unused references expire after 30 seconds; active remote mounts renew their
lease. Operations have a five-second response timeout, while caller cancellation
and local close interrupt waits immediately.
