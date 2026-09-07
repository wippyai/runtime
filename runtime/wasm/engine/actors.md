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
    options:
      worker_class: wasm
      limits:
        memory_bytes: 67108864
        host_buffer_bytes: 2097152
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

## Mailbox and socket selection

The actor interface also provides `subscribe() -> pollable`. It observes mailbox
readiness without removing a message; the guest calls `try-receive` or `receive`
to take the message after polling. Readiness is level-triggered, and mailbox
closure wakes a pending waiter. Dropping the subscription leaves the mailbox
alive. Mailbox and socket subscriptions use the same per-instance WASI resource
table, so the guest can pass both to one WASI poll call.

The guest world must import `wasi:io/poll@0.2.8`; the runtime entry enables it
with these host profiles:

```yaml
imports:
  - wippy:actor
  - wasi:poll
```

Socket guests also need `wasi:sockets` and `wasi:io` for the interfaces they use.
`wasi:io` alone does not register poll. A manually registered actor host must
receive the instance's shared resource table. The legacy zero-argument host
constructor supports the original messaging methods but rejects `subscribe`
without that table.

## Limits and placement

For `process.wasm`, execution controls are `options.worker_class`,
`options.limits`, and `options.mailbox`. For `function.wasm` and `function.wat`,
limits are `options.limits`; root `pool` remains the function pool path.
Control groups under `meta.options`, and flat `limits`, remain compatibility
aliases with admission warnings. Aliases for the same group cannot be combined
in one declaration; a later deployment layer may override an earlier one.
Function invocation defaults under `meta.options` retain their existing behavior.
A `process.wasm` root `pool` remains invalid. Code, entrypoint, imports, WIT,
WASI mappings, and transport remain at the entry root.

Actor `max_execution_ms` limits total lifetime, including parked time; zero means
indefinite lifetime. Function `max_execution_ms` remains a per-call deadline.
Both actor and function runtimes interrupt a tight guest loop on cancellation or
execution deadline by closing its instance. This is not
resumable preemption. WASM actors therefore require a `wasm` process host, whose
workers use dedicated OS threads and the reserved WASM CPU set when affinity is
enabled. CPU isolation without an enabled affinity partition is not guaranteed.

`memory_bytes` caps aggregate logical guest linear-memory bytes per actor,
including unexported memories and repeated core definitions. Imported aliases
are counted only once. Initial memory is admitted before guest initialization;
later growth is denied when the remaining budget is insufficient. The per-core
compiler ceiling remains an additional bound. This does not measure backing
capacity, transient growth copies, compiled code, host buffers, or Go heap/RSS.
Memory charges are released after owned modules close; a canceled shutdown join
retains them until shutdown is retried successfully.

`host_buffer_bytes` independently caps explicitly accounted resident host-buffer
capacity. Omission or zero adds no extra host-byte ceiling; negative values are
invalid. The current charge is two 64 KiB TCP rings per connected socket. Both
reservations precede ring allocation; failures close the connection and return
`out-of-memory` without publishing a stream tuple. A socket retains its full
128 KiB charge until both pumps have exited, including when stream handles were
dropped earlier. Each PID has its own budget. These semantics are independent of
guest linear memory, mailbox limits, and socket-count admission. The ceiling
currently excludes UDP buffers, HTTP buffers, filesystem buffers, legacy core
socket allocations, Go objects/stacks, and kernel buffers; it is not an RSS cap.

Mailbox budgets include envelope overhead (256 bytes per message and
64 bytes per payload, plus encoded data and strings). A single message must fit
both the per-message and total mailbox budgets. Runtime lifecycle signals use
the existing reserved system channel, separate from application-message budgets.

Each actor's WASI resource table additionally caps live handles at 4096 and counts
Preview2 TCP/UDP sockets and legacy core `socket` connections together against
`max_open_sockets`. Reservations remain held until the underlying socket closes;
failed operations release them. Each actor owns an independent budget. Closing
the resource scope prevents late resource publication.
`socket_timeout_ms` applies to the core socket profile and Preview2 TCP
connect/listen startup and Preview2 UDP bind startup. A startup deadline stops the network job; it does not
expire an established connection or listener while the guest delays finish or
waits for clients. Explicit TCP blocking read, skip, write-and-flush,
write-zeroes-and-flush, flush, and splice use one absolute deadline per host
operation, including dispatcher waits. Expiry aborts the owning TCP socket and
joins both network pumps, even when the connection rejects deadline setters.
The guest receives `last-operation-failed` with an owned timeout error; subsequent
stream operations report closed. A successful completion stays successful if
the guest resumes after the deadline. Generic poll and idle accept remain
indefinite. Preview2 UDP receive/send operate on bounded host queues and never
wait for network I/O. Managed DNS lookups use the same socket timeout; mixed
splices cannot bound a synchronous non-TCP resource's own blocking implementation.
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
These TCP rings count against `host_buffer_bytes` when configured, and remain
bounded by socket count otherwise. They never consume `memory_bytes`. Listening sockets use a fixed accept ring (up to 128 queued connections) and live readiness.
The accept pump reserves socket quota before entering the OS accept call;
queued and in-flight accepts count alongside guest-owned sockets. Empty accept
returns `would-block`. Closing the listener joins the pump and closes queued
connections before releasing quota; dropping a subscription does not close it.
Multi-source selection currently uses Go reflection in the dispatcher wait,
outside the actor messaging path.

UDP sockets use one reader and one writer pump with at most 16 queued datagrams
per direction; the outgoing limit includes the in-flight write. Each payload is
at most 65,535 bytes. The reader additionally owns a 65,535-byte scratch buffer
and may hold one received packet while the queue is full. These buffers are
bounded by socket count, not charged to `memory_bytes`. Closing a socket joins
both pumps and any pending bind before releasing its quota.

UDP `receive` returns an empty success when idle. `check-send` grants a one-use
permit; sending without a sufficient permit traps. `send` reports packets
accepted into the queue, and a later packet error preserves the accepted count.
Connected streams restrict destinations and filter incoming peers. Both stream
association and send check `socket.connect`; private, loopback, and link-local
addresses also require `socket.private_ip`, matching TCP dialing policy. Stream
subscriptions reflect queue readiness and wake on stream drop. Canonical send
preflight caps batches at 16 records and 1,048,560 payload bytes before copying
guest memory. Ordinary oversized packets return `datagram-too-large`; batches
exceeding the host byte budget trap. This budget is an explicit host limit.

The standard WASI UDP fixture verifies idle receive, real loopback echoes,
zero-length datagrams, and cancellation cleanup through embedded Asyncify. A
client ACK confirms delivery before the guest drops its socket; queued sends
alone do not establish delivery.

UDP bind honors the selected network provider and never silently falls back to
the host network. The current actor transport requires a native `*net.UDPConn`;
other packet providers return `not-supported` and their results are closed.
General overlay datagram adapters remain future work.

## DNS resolve streams

DNS name input is capped at 1,024 UTF-8 bytes before canonical string lifting.
Names are validated and converted with IDNA; invalid names return
`invalid-argument`, while input exceeding the host byte limit traps. IP literals
produce a ready stream without calling a resolver. Lookup results use canonical
`ip-address` variants without ports, with IPv4-mapped IPv6 normalized to IPv4.
`socket.resolve` authorization uses the normalized name. A selected network
provider handles hostname lookup; missing or failed overlays do not fall back
to host DNS.

Hostname resolution returns a stream after dispatcher startup acknowledgement,
without waiting for lookup completion. `resolve-next-address` returns
`would-block` while pending; subscriptions signal completion or failure and
remain ready at EOF. A snapshot holds at most 64 addresses and 4,096 total
address-string bytes. Exceeding either snapshot limit returns `out-of-memory`;
these limits bound retained runtime snapshots, not allocations made inside a
network provider. Successful empty lookup results become `name-unresolvable`.

The stream owns its pending lookup until drop, even after results are consumed.
Drop cancels and joins the operation and any in-flight result disposal before
clearing references. Dropping a subscription leaves the stream alive. Startup
uses `socket_timeout_ms`; a later poll does not restart the deadline. Resource
handles bound simultaneous streams, and these host snapshots are not charged
to `memory_bytes`.

The real Rust DNS fixture validates parallel lookups, IDNA normalization,
literal handling, pending readiness, IPv4/IPv6 results, deadlines, cancellation,
and cleanup. It exposed an embedded Asyncify local-liveness defect: a skipped
assignment could incorrectly suppress saving a live resource handle. The backend
now computes liveness across control-flow joins and loop backedges, preserving
selective saves. Both a minimal unwind/rewind regression and the DNS guest pass.

## Scope and measurements

The actor fixture covers repeated receive/send, retained state, PID identity,
empty nonblocking receive, denied sends, and mailbox arrivals during a pending
send. Backend regressions cover conditional branches, rewind routing, loop-local
liveness, indirect canonical results, post-return cleanup, cancellation, and
resource ownership. Actor runtimes are isolated. A factory generation shares compiled
machine code across those runtimes; guest memory and host resources remain separate.
The cache stays alive while the factory, a pending spawn or a live actor retains it.

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

Actor performance measurements are diagnostic and exclude outer scheduler
routing and transport; they make no indexer-performance or sustainable-capacity
claim.

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
uniform network deadlines, aggregate host memory accounting, and application
workload measurements including outer scheduler routing. Nonzero IPv6 flow-info is
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

A concurrent standard-WASI TCP echo fixture serves eight loopback clients using
one bounded frame buffer per connection and a stack list of at most nine
pollables. An integration test stalls one client halfway through a frame while
the other seven finish; a partial-EOF test covers error cleanup. Both check socket
reservations return to zero. `BenchmarkConcurrentTCPEcho` uses the same clients
against this guest and a Go reference. It includes real socket dispatch and
transport, excludes compile/setup and outer process-scheduler routing, and
reports aggregate throughput cost separately from measured client RTT.
Allocation counts include clients and the driver. Source and reproduction
instructions are in `testdata/concurrent_tcp/README.md`.

The eight-client TCP benchmark measures 37.31–38.06 microseconds per echoed
64-byte frame in aggregate throughput, with 287.6–299.2 microseconds mean client
RTT, approximately 5.59–5.67 KB and 111–112 whole-harness allocations per frame.
The Go reference measures 30.68–32.03 microseconds throughput cost and
238.6–253.5 microseconds mean RTT. Before correcting GC-unsafe list backing,
typed nonblocking stream bindings measured 96–97 allocations and
37.74–38.08 microseconds per frame. The earlier untyped bindings measured
152–154 allocations. The safety fix increases allocations; these shared-machine
samples do not establish a throughput improvement. Both current benchmarks use
three two-second samples. Nonblocking read, skip, check-write, write, and flush
use typed adapters with the existing bounds checks and stream-error semantics.
