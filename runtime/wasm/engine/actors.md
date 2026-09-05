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
TCP and UDP sockets together against `max_open_sockets`. Dropping or closing
resources releases their capacity. The legacy core `socket` profile has its own
socket table and limit; using both socket APIs does not provide a combined quota.
`socket_timeout_ms` currently applies to the core socket profile. Preview2 socket
operations inherit process cancellation but still need uniform operation timeouts.

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

The counter benchmark includes the real Rust guest, ingress copying, Canonical
ABI, and Asyncify resumption. With the production cancellation setting enabled,
the same-machine baseline was approximately 13–14 microseconds, 7.4 KB, and 164
allocations per round trip. The optimized path measures approximately 9
microseconds, 5.3 KB, and 114 allocations. Scheduler routing and network transport
are excluded; this is not a native-indexer comparison. The older 11-microsecond /
123-allocation measurement used a fixture with cancellation checks disabled and
must not be used as the production baseline. Wazero's per-call cancellation
watchers remain the largest allocation source in the optimized profile.

This enables a stateful indexing actor; it does not implement an indexer or replace
Wippy's Lua compiler/type system. MQTT remains a follow-up server workload:
Preview2 multi-source blocking poll, uniform network deadlines, combined host
resource accounting, and a real server/concurrency benchmark are not validated
by the counter fixture. Do not treat this PR as production MQTT support.
