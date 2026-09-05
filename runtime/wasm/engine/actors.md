# Stateful WASM processes

`process.wasm` creates one guest execution per PID. Guest globals and linear
memory survive mailbox waits and dispatcher yields. The process host owns the
execution; closing it releases the guest instance, its dedicated backend runtime,
and its host resource table. Updating an entry affects future spawns. Existing
actors continue under their process host and supervisor.

```yaml
entries:
  - name: wasm_host
    kind: process.host
    host:
      workers: 2
      worker_class: wasm

  - name: indexer
    kind: process.wasm
    meta:
      options:
        worker_class: wasm
        limits:
          memory_bytes: 67108864
          max_execution_ms: 0
          max_open_sockets: 16
          socket_timeout_ms: 30000
        mailbox:
          capacity: 128
          bytes: 8388608
          message_bytes: 1048576
    fs: app:assets
    path: indexer.wasm
    hash: sha256:<component-content-hash>
    method: run
    imports:
      - wippy:actor
```

`imports` contains runtime host-profile IDs. `wippy:actor` is a registered profile
which provides the WIT interface `wippy:actor/process@0.1.0`; the profile name is
not itself a WIT declaration. The interface and example world are in
`../host/wippy/hosts/actor/actor.wit`. The component may use the existing `wasi:*`
profiles for explicitly granted host capabilities.

## Portable semantics

- `self` returns the real runtime PID. `send` checks `process.send` with that PID
  and the current security scope. Guest-supplied message data cannot choose the
  sender identity.
- `receive` parks when the inbox is empty; it uses no dispatcher command or
  polling loop. `try-receive` returns immediately with an optional message.
- A successful send means the destination accepted the message. It does not
  acknowledge processing or durable storage. Overload is an explicit error;
  there is no implicit retry. Applications can add request IDs and acknowledgments.
- The inbox is FIFO in admission order. A relay batch is accepted atomically.
  Budgets cover both scheduler ingress and the delivered inbox. Messages arriving
  during another yield remain queued until the guest resumes receiving.
- Payloads cross as encoded bytes, UTF-8 text, or UTF-8 JSON. Lua object handles
  and arbitrary object graphs cannot cross. Ingress takes an owned snapshot.
  Host-call preflight rejects oversized canonical `send` arguments before the
  decoder allocates Go slices.
- Guest return values follow the existing payload transport. Guest traps and
  cancellation fail the process. A returned WIT `result` is a return value, not
  implicitly a process trap; guests should choose their failure convention.

These contracts do not depend on the W1 scheduler implementation and are intended
to survive the W2 port. W2 code is not changed by this work.

## Limits and placement

Execution controls belong in `meta.options` for `process.wasm`, `function.wasm`,
and `function.wat`. Code, entrypoint, imports, WIT, WASI mappings, and transport
remain at the entry root. Functions put `pool` and `limits` under `meta.options`.
Actors reject pool settings. Root-level `pool` and `limits` are rejected with a
migration error, including empty or null values. Explicit zero retained-memory
recycling limits on functions preserve their existing meaning.

Actor `max_execution_ms` limits total lifetime, including parked time; zero means
indefinite lifetime. Function `max_execution_ms` remains a per-call deadline.
Both actor and function runtimes interrupt a tight guest loop on cancellation or
execution deadline by closing its instance. This is not
resumable preemption. WASM actors therefore require a `wasm` process host, whose
workers use dedicated OS threads and the reserved WASM CPU set when affinity is
enabled. CPU isolation without an enabled affinity partition is not guaranteed.

`memory_bytes` is a ceiling per linear memory, not an aggregate component or Go
heap quota. Mailbox budgets include envelope overhead (256 bytes per message and
64 bytes per payload, plus encoded data and strings). A single message must fit
both the per-message and total mailbox budgets. Runtime lifecycle signals use
the existing reserved system channel, separate from application-message budgets.

Each actor's WASI resource table additionally caps live handles at 4096 and counts
Preview2 TCP/UDP sockets and legacy core `socket` connections together against
`max_open_sockets`. Reservations remain held until the underlying socket closes;
failed operations release them. Each actor owns an independent budget. Closing
the resource scope prevents late resource publication.
`socket_timeout_ms` applies to the core socket profile and Preview2 TCP
connect/listen startup. A startup deadline stops the network job; it does not
expire an established connection or listener while the guest delays finish or
waits for clients. Explicit TCP blocking read, skip, write-and-flush,
write-zeroes-and-flush, flush, and splice use one absolute deadline per host
operation, including dispatcher waits. Expiry aborts the owning TCP socket and
joins both network pumps, even when the connection rejects deadline setters.
The guest receives `last-operation-failed` with an owned timeout error; subsequent
stream operations report closed. A successful completion stays successful if
the guest resumes after the deadline. Generic poll and idle accept remain
indefinite. UDP and DNS still need uniform operation timeouts; mixed splices
cannot bound a synchronous non-TCP resource's own blocking implementation.
Legacy dispatcher accept commands
close their listener on process cancellation and release unadopted connections;
WASM listener accepts now use the socket-owned queue described below.

`wasi:io/poll.poll` suspends until a timer or notifying resource is ready, with
one dispatcher wait per suspended poll and no worker blocking. Inputs are capped
at 4096 before canonical list lifting; empty lists and invalid handles trap.
Manual pollables expose live signals, wake on resource drop, and do not fabricate
readiness when blocked. Single timer `pollable.block` retains the clock path.
TCP input and output stream resources now have live subscriptions and fixed
64 KiB rings per direction. At most one reader and one writer pump own the
network calls for each connected socket; guest reads and writes only copy buffered
data. Blocking input and output operations suspend through the dispatcher, with
write completion retained across rewind so flushing cannot repeat a write.
Subscriptions borrow their stream; dropping a subscription leaves the stream
alive. Socket close stops and joins both pumps before returning socket quota.
These host buffers are bounded by socket count but are not charged to
`memory_bytes`. Listening sockets use a fixed accept ring (up to 128 queued connections) and live readiness.
The accept pump reserves socket quota before entering the OS accept call;
queued and in-flight accepts count alongside guest-owned sockets. Empty accept
returns `would-block`. Closing the listener joins the pump and closes queued
connections before releasing quota; dropping a subscription does not close it.
Multi-source selection currently uses Go reflection in the dispatcher wait,
outside the actor messaging path.

## Scope and measurements

The actor fixture covers repeated receive/send, retained state, PID identity,
empty nonblocking receive, denied sends, and mailbox arrivals during a pending
send. Backend regressions cover conditional branches, rewind routing, loop-local
liveness, indirect canonical results, post-return cleanup, cancellation, and
resource ownership. Actor runtimes are isolated; cold spawning currently recompiles
inside a fresh runtime.

Actor messaging uses typed host invocation and compiled canonical result plans.
`send` has an explicit completion continuation that avoids decoding the original
arguments again on rewind. Bounds checks, payload snapshots, authorization, and
cancellation remain enabled. Temporary memory/allocator wrappers are returned to
a pool after each host invocation with all instance/context references cleared.
Successful Asyncify control transitions use mutable globals only when trusted
linker/engine metadata proves that this load generated the controls. Invalid
transitions, bounds traps, cancellation, and closed modules use the original
functions to preserve their effects. Externally transformed and guest-authored
controls retain the function path. Regular guest execution keeps cancellation
instrumentation enabled.

The counter benchmark includes the real Rust guest, ingress copying, Canonical
ABI, and Asyncify resumption. With the production cancellation setting enabled,
the same-machine baseline was approximately 13–14 microseconds, 7.4 KB, and 164
allocations per round trip. The optimized path measures 6.32–6.37 microseconds,
approximately 3.25 KB, and 69 allocations. This pass measured 6.69–6.75
microseconds, 3.86 KB, and 82 allocations before consolidating immutable call
contexts, skipping fully overridden canonical wrappers, forwarding on the
existing call stack, and storing actor send commands in their continuations.
A sequential before/after comparison
of the generated-control optimization measured 8.48–8.59 microseconds, 5.34 KB,
and 114 allocations before the change. Scheduler routing and network transport
are excluded; this is not a native-indexer comparison. The older 11-microsecond /
123-allocation measurement used a fixture with cancellation checks disabled and
must not be used as the production baseline. Regular guest calls still incur
Wazero's per-call cancellation watchers. Linker module wrappers remain immutable;
only canonical imports with both explicit memory and realloc bindings skip the
shared fallback wrapper. Partially bound imports preserve that fallback.

The checked-in standard WASI 0.2.8 TCP component exercises canonical IPv4
addresses, socket creation, connect, buffered ping/pong, connection-refused error
encoding, and resource drops through the actual process and embedded Asyncify
path. Tests supply a controlled connection at the dispatcher boundary and verify
socket quota is returned. The fixture can be rebuilt byte-for-byte from its
locked Rust sources. This covers neither OS routing nor a real listener workload.

TCP connect and listen use separate start and finish operations. Start dispatches
the network job and returns after acknowledgement; finish reports `would-block`
until completion, and socket subscriptions expose readiness. Each pending job
belongs to its socket before dispatch. Finish transfers its result into that
socket; dropping the socket cancels and joins the job before releasing quota.
A second TCP guest starts two connections before either completes, then polls
and finishes both. Tests also close its resource scope with both dials pending
and verify that late connections close and all socket reservations return.

This enables a stateful indexing actor; it does not implement an indexer or replace
Wippy's Lua compiler/type system. Production server support still needs
uniform network deadlines, aggregate host memory accounting, and a real
server/concurrency benchmark. Nonzero IPv6 flow-info is
explicitly unsupported; numeric scope IDs are preserved. Do not treat this PR
as production MQTT support.

A standard-WASI Rust fixture runs a limited MQTT 3.1.1 server through
real loopback TCP and socket dispatcher commands. It handles CONNECT/CONNACK,
PINGREQ/PINGRESP, and DISCONNECT for two sequential clients, retaining the
served-client count in guest state. Tests force polling during listen startup
and an empty-listener poll before
allowing clients to connect, reject an oversized frame, and close the resource
scope while accept is parked. Every case returns all socket reservations.
This fixture is not a broker: it lacks publish/subscribe, concurrent clients,
authentication, and keep-alive enforcement, and supplies no broker performance
claim. Its source, locked dependencies, and exact supported protocol are in
`testdata/mqtt/README.md`.
