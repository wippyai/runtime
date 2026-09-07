# SQLite actor workload

This harness routes requests through Wippy's production process host, dedicated
WASM scheduler workers, relay router, and process dispatcher. Each actor owns an
in-memory SQLite database. It does not manually drive `ActorProcess.Step`.

The workload is under development. With the local Asyncify repairs, the
10,000-row diagnostic and production actor correctness tests (one actor and
four concurrent actors) pass on stock Wazero. Restoring the old `local.tee`
operand aliasing reproduces database corruption. Native SQLite, untransformed
WASM, and Binaryen-transformed WASM also pass the diagnostic. These backend
repairs are not yet included in the published dependency pin.

A local stock-Wazero run completed 780,613 requests in 30 seconds (26,020 ops/s)
with four actors, 100,000 rows per actor, and four closed-loop clients. There
were no request errors and final database oracles passed. End-to-end latency
was p50 147 microseconds, p95 239 microseconds, and p99 348 microseconds,
estimated from a 20,000-sample reservoir. This single development-machine run
is preliminary evidence, not a deployment capacity guarantee. That run used
the earlier harness configuration with strict security checks disabled.

The SQLite hardening checks now cover explicit SQLite out-of-memory responses
under a 2 MiB guest limit, recovery in the same actor, cancellation after an
observed successful B-tree insertion, repeated graceful exits, and typed mailbox
rejection followed by draining and recovery. The cancellation test uses a
Wazero function listener only in that test; the load measurement does not.
These checks do not establish general leak freedom or resumable preemption.

## Workload and measurement scope

The load test uses independent actor databases, deterministic client row
partitions, 80% reads and 20% writes. It verifies every returned ID and value,
updates its reference state after acknowledged writes, and checks final row
counts and sums. Requests already offered get a separate bounded completion
window when the offering interval ends.

Latency starts before relay submission and includes queueing, guest execution,
and reply delivery. Percentiles use a bounded 20,000-sample reservoir. Concurrency
is bounded and the workload uses closed-loop clients; it does not establish
capacity under arbitrary open-loop arrival rates. Go heap measurements include
the harness, compiler, and runtime; SQLite allocator statistics cover guest
allocations. A single post-GC sample is not evidence of leak freedom. Compilation
and database load are measured separately from sustained request execution.

The harness now uses strict security and a scoped policy allowing actor replies
only to currently registered clients on its local client host. Other actions and
reply destinations are denied. This policy represents this workload, not a full
application authorization model. Wazero cancellation checks remain enabled. There is
no resumable preemption. No native indexer, full-text search, durable database,
or broker performance claim follows from this workload.

## Protocol

Requests are topics; responses have topic `result` and one UTF-8 text payload.
Errors use topic `error`. The fixture README documents build inputs and limits.

| Request | Successful response |
| --- | --- |
| `load:N` | `loaded:N` |
| `get:ID` | `value:ID:VALUE` |
| `put:ID:VALUE` | `updated:ID:VALUE` |
| `sum` | `sum:COUNT:SUMVALUE` |
| `stats` | `stats:USED:HIGHWATER:PAGE_COUNT:PAGE_SIZE` |
| `stop` | Actor exits without replying |

Once the backend passes correctness checks, run the sustained test with
`WIPPY_SQLITE_LOAD=1 go test ./runtime/wasm/engine -run '^TestSQLiteActorLoad_Sustained$' -count=1 -v`.
Defaults are 100,000 rows per actor, four actors, four clients, and 30 seconds.
`WIPPY_SQLITE_LOAD_ROWS`, `WIPPY_SQLITE_LOAD_ACTORS`,
`WIPPY_SQLITE_LOAD_CONCURRENCY`, and `WIPPY_SQLITE_LOAD_DURATION` configure these
within the bounds enforced by the test.

## Open-loop load proof

`sqlite_open_loop_test.go` extends the SQLite actor evaluation with an open-loop
arrival generator independent of request completions. Unlike closed-loop drivers
where slow completions throttle subsequent offers (coordinated omission), the
open-loop generator produces planned arrivals at scheduled nominal intervals
$t_k = t_0 + k \times \Delta$ (where $\Delta = 1/R$).

### Architecture and bounded queues

- **Independent arrival generator**: Operates on absolute scheduled offer times.
  Total scheduled arrivals are strictly predetermined by declared rate and duration:
  $N_{\text{planned}} = \lfloor R \times T \rfloor$.
  If the generator falls behind wall clock time near window close, missed arrivals are
  accounted as `generatorDropped` rather than disappearing from planned arrivals.
  Generator lateness ($t_{\text{actual}} - t_k$) is recorded into a bounded reservoir.
- **Fixed worker pool & bounded queue**: A fixed set of client workers ($W$) pulls
  from a bounded work channel ($Q$). No goroutines are spawned per offered request.
- **Dedicated per-actor worker clients & FIFO delivery guarantees**: Each worker maintains dedicated client
  connections and channels per actor ($W \times A$). This eliminates cross-actor message
  multiplexing and ensures replies from one actor never arrive on channels designated for another.
  Each client's reply channel is buffered to 1024 messages (`chan *relayapi.Message, 1024`).
  `harnessClientReceiver.Send` delivers incoming replies via nonblocking `select`, incrementing
  `receiver.dropped` if the buffer overflows. Because FIFO correlation strictly requires
  that every admitted request's reply is received in order without omission, any dropped reply
  would desynchronize the pending queue. The harness asserts `require.Zero(t, cluster.receiver.dropped.Load())`.
- **Bounded pending queues with reference clearing**: Every admitted request is enqueued in an ordered FIFO
  pending queue per (worker, actor). `pendingQueue` enforces an explicit capacity cap (`maxPending`, default 64).
  To eliminate memory retention, `pendingQueue.pop()` explicitly clears the vacated slot pointer (`q.ops[0] = nil`)
  before slicing, and resets slice capacity (`q.ops = q.ops[:0]`) upon becoming empty. Residual drain explicitly invokes `queue.clear()`.
- **Pending cap backpressure accounting BEFORE admission**: Before admitting a request to the actor router,
  workers drain ready replies on `client.replyCh`. If the pending queue is at capacity (`queue.isFull()`),
  the worker waits on `client.replyCh` for late replies to arrive (freeing pending slots) or for the request's
  deadline to expire. If the deadline expires while waiting for pending capacity, the request fails admission
  due to backpressure (`failedAdmission` and `failed` incremented, `endToEndLatency` recorded) before dispatch.
  Late replies continue to be drained and validated against the oracle; no operations are forgotten.
- **Queue wait & end-to-end latency**: Per-request deadline is measured from the
  scheduled offer timestamp ($t_k + \text{reqTimeout}$). Requests that expire in the
  work queue before worker dequeue are not admitted to the actor mailbox and are counted
  as unadmitted queue timeouts (`failedAdmission`). Latency reservoir captures dispatched
  requests from scheduled offer timestamp ($t_{\text{done}} - t_k$). Generator drops are
  omitted from this distribution and tracked separately via count and lateness reservoir.
- **Backpressure & drops**: If the queue is saturated, requests drop immediately at the
  generator (`generatorDropped`). If the actor's mailbox capacity is saturated, admission
  fails with typed `actorhost.ErrOverloaded` (`failedAdmission`).
- **Refactored drain helper with worker join**: Worker draining and teardown is encapsulated
  in `waitOrCancelWorkers`. If `cfg.drainBudget` expires, worker cancellation contexts are signaled,
  and the helper waits for all cancellation-aware workers to exit on the *same* existing `drainDone` channel
  without spawning duplicate waiters. Workers are guaranteed joined before returning, preventing orphaned goroutines
  or background memory corruption. Go's outer test timeout bounds unrecoverable hangs.
- **Bounded semantic error storage**: Semantic error records are capped at 64 entries
  with an atomic counter for dropped errors, preventing unbounded heap growth during failure storms.

### Strict conservation accounting

Every request is tracked through explicit lifecycle transitions satisfying six
simultaneous conservation equations against the predetermined schedule:

1. $\text{planned} = \text{offered} + \text{generator\_dropped}$
2. $\text{offered} = \text{admitted} + \text{failed\_admission}$
3. $\text{admitted} = \text{completed} + \text{failed\_execution} + \text{timedout}$
4. $\text{failed} = \text{failed\_admission} + \text{failed\_execution}$
5. $\text{offered} = \text{completed} + \text{failed} + \text{timedout}$
6. $\text{planned} = \text{completed} + \text{failed} + \text{timedout} + \text{generator\_dropped}$

Semantic mismatches, malformed payloads, or unexpected guest errors increment
`failedExecution` and immediately fail the test. Overload permits admission backpressure
(`failedAdmission`) and request deadlines (`timedout`), but never data corruption or
malformed results (`failedExecution == 0`).

### Mixed GET/PUT reference oracle and reconciliation

The open-loop harness executes a mixed workload (80% GET, 20% PUT) under concurrency:

- **Partitioned key space**: Row IDs are partitioned by worker ($id \pmod W = workerID$),
  preventing write-write races across workers without lock contention. Configuration enforces
  $\text{rowsPerActor} \ge W$ so no partition is empty or falls back to row 0.
- **Correlated reply oracle**: Every incoming reply is correlated with the head of the
  worker's per-actor pending queue.
  * Exact PUT validation: When `updated:ID:VAL` arrives, the oracle verifies that `ID == pending.rowID`
    and `VAL == pending.sentVal`. Arbitrary or corrupted PUT replies immediately trigger `replySemanticError`.
  * Monotonic write sequencing: Each write is stamped with a monotonic write sequence number.
    Late ACKs from older timed-out writes never overwrite newer acknowledged state.
  * Exact GET verification: GET values are strictly verified against the legal reference state:
    - Clean row: $V == \text{rowID} \times 3$
    - Acknowledged row: $V == \text{acked}[\text{rowID}]$
    - Uncertain row: $V \in \text{candidateSet}[\text{rowID}]$
- **Isolated residual drain**: After the work queue drains, each worker drains each actor's
  channel independently against that actor's pending queue. Replies are never broadcast across actors.
- **Acknowledged writes**: When a PUT returns `updated:ID:VALUE`, the reference oracle
  records the new value authoritatively and removes any uncertainty for that row provided it is not superseded.
- **Uncertain writes**: When a write is admitted to SQLite but times out waiting for
  client ACK, cancellation is never assumed to roll back. The row is marked uncertain with
  candidate set $\{V_{\text{prev}}, V_{\text{new}}\}$. Repeated timeouts on the same row
  accumulate candidate values without overwriting or forgetting admitted writes.
- **Per-key final ACK verification & reconciliation**: After queue drain, reconciliation:
  1. Reconciles all uncertain keys via `get:ID`, asserting values belong to candidate sets.
  2. Queries `get:ID` for every touched ACKed row, verifying exact cell values.
  3. Computes the exact expected final sum and checks `sum` from the SQLite actor.

### Memory time-series sampling

- Sampled at 50ms intervals by a dedicated background monitor outside the request hot path.
- Records `HeapAlloc`, `HeapInuse`, `Sys`, and `NumGC` across baseline, peak, end-of-load,
  and post-cleanup stages.
- Calculates heap growth slope ($\Delta\text{HeapAlloc}/\Delta t$) over quiescent windows.
- Enforces no arbitrary leak thresholds; Go and Wazero virtual page retention is reported.

### Deterministic fault-injection unit tests

Deterministic unit tests verify error handling, accounting, and correlation without hardware speed dependency:
- `TestSQLiteOpenLoop_Oracle_RejectsBadGetValue`: Proves wrong GET values, stale reads, and unexpected topics are flagged as semantic errors.
- `TestSQLiteOpenLoop_Oracle_UncertainRepeatedWritesNotForgotten`: Proves repeated timeouts on the same row preserve all candidate values until ACKed.
- `TestSQLiteOpenLoop_Accounting_MissingArrivalsCounted`: Proves predetermined scheduled arrivals missed by late generators are counted as `generatorDropped`.
- `TestSQLiteOpenLoop_Deadlines_Bounded`: Proves queue wait timeouts reject admission before router dispatch, and validates config constraints ($rows \ge workers$).
- `TestOracleCorrelation_RejectsArbitraryPutValue`: Proves corrupted or arbitrary PUT replies are rejected and do not mutate the reference oracle.
- `TestOracleCorrelation_LateOldAckMustNotRegressState`: Proves late older ACKs cannot overwrite newer acknowledged state.
- `TestOracleCorrelation_ResidualDrainIsolatesActors`: Proves residual drain isolates channels per actor without cross-actor state pollution.
- `TestOracleCorrelation_PendingQueueFIFOOrdering`: Proves FIFO ordering matches replies to original operations.
- `TestOracleCorrelation_UnsolicitedReplyRejected`: Proves unsolicited replies without matching pending requests are rejected.
- `TestOracleCorrelation_BoundedSemanticErrorStorage`: Proves error storage memory is bounded to 64 records.
- `TestOracleCorrelation_WaitOrCancelWorkers`: Proves the real `waitOrCancelWorkers` helper joins cleanly within budget and cancels/joins workers on the same `drainDone` channel upon timeout.
- `TestOracleCorrelation_PendingQueuePointerClearing`: Proves `pop()` and `clear()` zero underlying slice pointers and reset slice capacity to prevent memory retention.
- `TestOracleCorrelation_PendingCapBackpressureAndLateReplyDrain`: Proves per-worker pending cap enforcement, pre-admission draining of late replies, and backpressure admission timeouts without forgotten operations.
- `TestOracleCorrelation_FIFOClientReplyDropDetection`: Proves nonblocking reply channel capacity (1024), drop counting, and why dropped replies would desynchronize FIFO correlation.
- `TestSQLiteActor_OpenLoop_MultiActorCorrelation`: Proves multi-actor open loop load under actual SQLite execution.

### Running the tests

1. **Deterministic unit tests**:
   ```bash
   GOWORK=/tmp/w1-review.work go test -v -run '^TestSQLiteOpenLoop_' ./runtime/wasm/engine -count=1
   ```
2. **Short deterministic correctness & overload test** (runs in default test suite):
   ```bash
   GOWORK=/tmp/w1-review.work go test -v -run '^TestSQLiteActor_OpenLoop_CorrectnessAndOverload$' ./runtime/wasm/engine -count=1
   ```
   Under race detector:
   ```bash
   GOWORK=/tmp/w1-review.work go test -race -v -run '^TestSQLiteActor_OpenLoop_CorrectnessAndOverload$' ./runtime/wasm/engine -count=1
   ```
3. **Configurable sustained open-loop load test** (opt-in):
   ```bash
   WIPPY_SQLITE_OPEN_LOOP=1 GOWORK=/tmp/w1-review.work go test -v -run '^TestSQLiteActorLoad_OpenLoop_Sustained$' ./runtime/wasm/engine -count=1
   ```
   Configurable parameters (with bounds validation):
   - `WIPPY_OPEN_LOOP_RATE`: target arrival rate in req/s (1..10000, default 500)
   - `WIPPY_OPEN_LOOP_DURATION`: load duration (1s..5m, default 10s)
   - `WIPPY_OPEN_LOOP_WORKERS`: worker client count (1..64, default 4)
   - `WIPPY_OPEN_LOOP_QUEUE`: bounded queue size (1..4096, default 256)
   - `WIPPY_OPEN_LOOP_ROWS`: rows per actor (1..1000000, default 10000)
   - `WIPPY_OPEN_LOOP_ACTORS`: actor count (1..16, default 2)
   - `WIPPY_OPEN_LOOP_TIMEOUT`: per-request timeout (10ms..30s, default 2s)
   - `WIPPY_OPEN_LOOP_DRAIN`: drain budget (1s..60s, default 10s)

### Scope and remaining gaps

This workload verifies scheduler open-loop arrival accounting, bounded queueing,
backpressure, timeout handling, and database consistency under overload. It does not
substitute for a frozen dedicated-host benchmark run and cannot establish production
capacity bounds.
