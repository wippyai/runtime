# Runtime Channel-Anchored Subscription Leak — Fix Plan

Branch: `fix/runtime-channel-leak`
Base: commit `46d62b1c` (production at time of triage on 2026-05-23)

## Problem

Long-running Lua processes (scheduler, job_worker, job_lease_heartbeat, similar)
accumulate gigabytes of memory over hours. Root cause is in the runtime's per-call
topic subscription model used by `time.*`, `websocket`, `tty`, `events`, `funcs`,
`contract`.

Every call to `time.after(d)` (and siblings):

1. Allocates a fresh `engine.Channel`.
2. Generates a unique topic string `"after@<counter>"`.
3. Calls `proc.SubscribeExisting(topic, ch)` — inserts into `proc.subs.byTopic`
   AND `proc.subs.byChannel`.
4. Calls `proc.SetTopicHandler(topic, h)` — inserts into `proc.handlers`.
5. Calls `resource.GetStore(ctx).AddCleanup(func(){ result.Stop(); ... })` —
   discards the cancel handle, so the cleanup is frame-process lifetime.

Nothing removes 3 / 4 / 5. After N calls, the three maps and the frame store grow
without bound. Scheduler/job_worker call this every poll cycle. 100k iterations
≈ GBs of map entries + closure captures.

The same shape exists in `runtime/lua/modules/{websocket,tty,events,funcs,contract}`.

Pre-existing related bug in `runtime/lua/engine/process.go:835` and `:854`:
`deliverMessage` discards `ChannelResult.Block/Release/Updates` from external
sends and closes. Blocked receivers can hang, refcounts drift. `unlisten`
(`process.go:736`) has the same defect plus never removes the handler entry.

## Design — V4 (locked after four rounds of hostile review)

### Core idea

Replace per-call topic subscriptions with a **process-local ephemeral channel
router**. One well-known reserved topic, one routing path, channel-anchored
lifetime, epoch+gen for staleness detection across reuse.

### Three subscription lanes

| Lane | API | Topic | Lifetime | Storage |
|---|---|---|---|---|
| A — system fixed | `process.events()`, `process.inbox()` | `@pid/events`, `@pid/inbox` | Process | `subs.byTopic` directly |
| B — user pub/sub | `process.listen(topic)` / `process.unlisten(ch)` | User-named | Explicit | `subs.byTopic` directly |
| C — ephemeral | `time.*`, `websocket.*`, `tty.*`, `events.*`, `funcs.*`, `contract.*` | One reserved: `@pid/route` | Channel | `proc.router.entries` map under the single reserved route |

Rule for new modules: producer-bound + single subscriber + lifetime tied to one
channel → lane C. Any direct `SubscribeExisting` call outside engine package
requires review justification.

### Lane C mechanics

* `Process.router *ephemeralRouter` — lazily allocated; nil for processes that
  never use lane C.
* `ephemeralRouter` holds `epoch uint64`, `entries map[uint64]*epEntry`,
  `nextID uint64`.
* `Process.epoch` is monotonic per process incarnation. Never reset to a
  previously-used value.
* `EphemeralFrame { Epoch, ChID, Gen, HasValue, Value, Close }` envelope —
  single payload carried by the relay.
* Reserved topic `TopicEphemeral = "@pid/route"`. `deliverMessage` gets a
  top-level branch BEFORE `subs.match`:
  ```
  if qm.Topic == TopicEphemeral { return p.router.Route(qm); }
  ```
  The route is NOT a `subs` entry. No handler entry. No `HasSubscriptions` quirk.
  No preregistration cost for processes that don't use it.
* Routing on step thread: decode envelope → check epoch == router.epoch → check
  chID in entries → check gen matches entry.gen → if `HasValue`, `TrySend` to
  channel (apply ChannelResult) → if `Close`, `Close` channel (apply
  ChannelResult), delete entry, call `producerStop` (sync.Once).
* `TrySend` is nonblocking. Overflow policy per entry: `Drop` /
  `Coalesce` / `Close` (close channel with overflow error + cancel producer).
* Malformed frame / unknown epoch / unknown chID / stale gen → consume and drop.
  Never re-queue. Hard-edged.

### Generation per timer arm

`system/clock` today captures callback at create time. For reset to work, each
*arm* must capture its own scheduled generation in the closure (not read at
fire time). Reset = stop old `time.AfterFunc` + new `AfterFunc` whose closure
captures the new gen. Stale fires from old arm still execute but carry old gen
→ router drops.

### Producer start/stop ordering — tombstones

`system/clock/dispatcher.go` and equivalents:

```
reverseMap   map[{pid, epoch, chID}]uint64    // → internalID
pendingStops map[{pid, epoch, chID, gen}]struct{}   // consumed by late Start
```

* `handleTimerStart`: lock → if `pendingStops` has key, delete and complete
  yield with `errStoppedBeforeStart` (do NOT schedule). Else install reverseMap
  entry, unlock, schedule with closure capturing `{epoch, chID, gen}`.
* `handleStopByChID`: lock → if reverseMap has it, stop internal timer +
  delete. Else mark tombstone in `pendingStops`.
* Tombstone consumed by matching Start; sweeper only fires on truly-lost Starts
  (e.g. process crashed before Start dispatched) at very long TTL (≥ 60s).

### Lifecycle drain

`Process.drainEphemeralChannels` (idempotent, sync.Once per producerStop):

```
1. Bump router.epoch FIRST (or replace router).
2. Snapshot entries under lock; detach.
3. Outside lock: for each entry → producerStop(); Close channel (apply
   result via applyChannelResult).
4. Clear entries.
```

Called from:
* `Process.Init()` — pre-clear on pool reuse.
* `Process.clearExecution()` — after main task completes.
* `Process.Close()` — final teardown.
* `Process.Abort()` — NEW; called by scheduler/pool executor on `ctx.Done()`
  early returns. **Constraint: Abort must not race Step.** Abort only bumps
  epoch + emits producerStops + sets an abort flag; actual channel/task
  cleanup deferred to next Step or Close on the owner thread.

### Channel primitives

* `Channel.TrySend(value) *ChannelResult` — nonblocking. Handoff to receiver if
  one's waiting, push to buffer if room, otherwise return `Result` with no Block
  entries. Never creates a fake blocked-sender (`task=nil` in `sendq`).
* `applyChannelResult(*ChannelResult)` — process Updates (wake), Block/Release
  (refcount), and release the pooled result exactly once. Used by router AND by
  existing `deliverMessage` send/close paths AND by fixed `unlisten` path.

### Channel ownership

Channels are not thread-safe (`engine/channel.go:109` comment). Producers
(goroutines like clock callback, ws read loop) NEVER call `Channel.TrySend`
directly. They send `EphemeralFrame` via `relay.Send` to `@pid/route`. Channel
mutation happens only on the step thread inside `deliverMessage`.

### Synthetic source PID

Internal producers populate `relay.Package.Source` with a synthetic stable
identifier (e.g. `pid:internal:clock:<targetPID>:<epoch>:<chID>`) so relay
mailbox hashing distributes load across workers rather than collapsing on the
empty default.

## Six invariants (must be tested explicitly)

I1. **Epoch monotonic per process incarnation.** Never reset to a value used
    earlier for the same pid.

I2. **Drain order.** Bump epoch FIRST; then stop producers; then close
    channels; then clear entries.

I3. **Route + Drain on the same synchronization boundary** (step thread).
    Snapshot under lock if helper needs to do work outside the lock.

I4. **`producerStop` installed before producer can orphan.** Entry insertion
    in router precedes producer Start command dispatch.

I5. **Tombstone consumed by matching Start, not just TTL.** Late Start that
    finds its key in `pendingStops` aborts itself. TTL is memory cleanup for
    truly lost Starts only.

I6. **`OverflowClose` triggers `producerStop` exactly once.** Closing the
    channel alone is not enough; the upstream producer must be cancelled.

## PR sequence (one at a time, each independently shippable)

### PR 1 — channel result correctness (prereq, independently useful)

**Scope:**
* New `Process.applyChannelResult(*ChannelResult)` helper.
* Wire helper into `deliverMessage` send path (`process.go:835`).
* Wire helper into `deliverMessage` terminal-close paths (`:807`, `:827`, `:857`).
* Wire helper into `processSubscribeYields` unsubscribe path (`:736-746`).
* In the unsubscribe path: add `RemoveTopicHandler(topic)` step. Modify
  `subscribeContext.remove` (or add `removeAndReturnTopic`) so caller has the
  topic.
* Add `Channel.CanSend()` guard before `Send(nil,...)` in `deliverMessage` to
  avoid creating phantom blocked-sender entries on full subscriber channels.

**Tests:**
* `TestDeliverMessage_FullChannel_DoesNotBlock`
* `TestDeliverMessage_TerminalCloseWakesReceivers`
* `TestDeliverMessage_AppliesBlockReleaseRefcount`
* `TestUnlisten_RemovesSubsAndHandler` — `process.listen + unlisten` leaves
  `subs.byTopic`, `subs.byChannel`, AND `proc.handlers` empty.
* `TestUnlisten_WakesBlockedReceivers`

### PR 2 — `Channel.TrySend` primitive

**Scope:**
* New `Channel.TrySend(value) *ChannelResult`. Nonblocking: handoff to
  receiver if waiting, push to buffer if room, otherwise return result with
  no Block entries.
* Channel remains single-thread-owned. TrySend is for the step thread.

**Tests:**
* `TestTrySend_HandoffToWaitingReceiver`
* `TestTrySend_PushToBufferWithRoom`
* `TestTrySend_ReturnsNotSentOnFull`
* `TestTrySend_DoesNotCreateBlockedSender`
* `TestTrySend_OnClosedChannel`

### PR 3 — `ephemeralRouter` infrastructure (no producer migration yet)

**Scope:**
* New `runtime/lua/engine/ephemeral.go` with `ephemeralRouter` type:
  `Register`, `BumpGen`, `Route`, `Stop`, `Drain`.
* New `EphemeralFrame` envelope payload type.
* Reserved topic constant `TopicEphemeral = "@pid/route"`.
* `deliverMessage` top-level branch before `subs.match`.
* `Process.epoch`, `Process.router` fields.
* `Process.Init/clearExecution/Close` call `Drain`.
* `Process.Abort()` method (epoch-bump + stop signals only; cleanup on Step).
* Fake producers in tests.
* `HasSubscriptions()` unaffected (router not a sub).

**Tests:**
* `TestRouter_RegisterReturnsMonotonicChID`
* `TestRouter_DeliverValueFrame`
* `TestRouter_DeliverCloseFrame`
* `TestRouter_UnknownChIDDropped`
* `TestRouter_StaleEpochDropped`
* `TestRouter_StaleGenDropped`
* `TestRouter_MalformedFrameDropped`
* `TestRouter_DrainBumpsEpochFirst`
* `TestRouter_DrainCallsProducerStopSyncOnce`
* `TestRouter_DrainOnInitReuse`
* `TestRouter_AbortBumpsEpochWithoutMutatingChannels`
* `TestRouter_NoSubsEntry` — `subs.byTopic` stays empty when only router is
  used.
* `TestRouter_OverflowDrop_NoLeak`
* `TestRouter_OverflowCoalesce_ReleasesDiscarded`
* `TestRouter_OverflowClose_CallsProducerStopOnce`
* `TestRouter_ReservedTopicNotInSubsTopics`

### PR 4 — clock dispatcher: epoch/gen + stop-by-chID + tombstones

**Scope:**
* `api/clock`: add `Epoch uint64`, `Gen uint64` fields to
  `TimerStartCmd`, `TickerStartCmd`. Add `TimerResetCmd.Gen`. Add new
  `TimerStopByChIDCmd { TargetPID, Epoch, ChID }` and `TickerStopByChIDCmd`.
* `system/clock/dispatcher.go`:
  * `reverseMap map[stopKey]uint64` and `pendingStops map[stopKey]struct{}`.
  * `handleTimerStart` installs reverseMap entry BEFORE returning; checks
    pendingStops first.
  * `handleStopByChID` stops or tombstones.
  * Periodic sweeper.
* `system/clock/timer.go`: callback closure captures `epoch, chID, gen`;
  `Reset` is stop+new-AfterFunc with new captured gen.
* Producer relay packages carry synthetic source PID.

**Tests:**
* `TestClock_StartInstallsReverseMapBeforeReturn`
* `TestClock_StopBeforeStart_Tombstoned`
* `TestClock_LateStartConsumesTombstone`
* `TestClock_ResetCapturesNewGen`
* `TestClock_StaleArmFiresWithOldGen`
* `TestClock_SyntheticSourcePIDFanOut`
* `TestClock_StopByChIDIdempotent`

### PR 5 — migrate `time.after/timer/ticker` to router

**Scope:**
* `runtime/lua/modules/time/yields.go`:
  * Delete `SubscribeExisting`, `SetTopicHandler`, `store.AddCleanup`.
  * Lua function (`afterFunc`, `timerFunc`, `tickerFunc`) calls
    `proc.RegisterEphemeral(ch, convert, producerStop, policy)` on the step
    thread BEFORE yielding.
  * Yields carry `(epoch, chID, gen)`.
  * `time.timer:stop()`, `time.ticker:stop()` yield `TimerStopByChIDCmd` /
    `TickerStopByChIDCmd`.
  * `time.timer:reset(d)` bumps gen via `router.BumpGen` and yields
    `TimerResetCmd { Gen: newGen, Duration: d }`.
  * Overflow defaults: `time.after`/`time.timer` cap 1 = irrelevant;
    `time.ticker` cap 16 = `OverflowDrop` (user-configurable).

**Tests (THE LEAK TESTS):**
* `TestAfter_NoLeak_AfterReceive` — 100k iterations of `local ch =
  time.after(d); ch:receive()` → `proc.subs.byTopic == 0`,
  `proc.router.entries == 0`.
* `TestAfter_NoLeak_SelectLoses` — another case wins; channel never read.
* `TestAfter_NoLeak_Dropped` — `time.after(d)` called without storing the
  return value.
* `TestTimer_StopUnsubscribes`
* `TestTimer_FireThenReset_StaleArmDropped`
* `TestTimer_ResetDuringFireWindow_NoDoubleDeliver`
* `TestTicker_StopUnsubscribes`
* `TestTicker_DroppedWithoutStop_BoundedByProcessLifetime`
* `TestProcessUpgrade_StaleTimerDropped`
* `TestPoolReuse_StaleTimerDropped`
* `TestSchedulerStyleLoop_HeapBounded` — synthetic 5000-iteration loop,
  assert RSS / `runtime.MemStats.HeapInuse` delta bounded.

### PR 6 — migrate `websocket` to router

**Scope:** `runtime/lua/modules/websocket/yields.go` → router. `OverflowClose`
default. ws read loop is the producer; producerStop cancels it. Last because
this is the most concurrency-sensitive path.

**Tests:**
* `TestWS_NormalConnectionCleanup`
* `TestWS_DisconnectDeliversCloseFrame`
* `TestWS_SlowConsumer_OverflowCloseTriggersProducerStop`
* `TestWS_ProcessExitCancelsReadLoop`

### PR 7 — migrate `tty`, `events`, `funcs`, `contract`

Same pattern. One per PR or batched depending on review bandwidth.

## Out of scope (documented limits)

* **Workflow-class processes** (deterministic replay): wall-clock timers and ws
  are nondeterministic. Do NOT persist `chID/gen` in workflow history. Either
  disable router-backed sources for these or route through the existing
  completion-through-history mechanism. Separate design needed.
* **`process.listen` with caller-generated unique topic names** — PR 1 makes
  `unlisten` actually work. If caller forgets `unlisten`, that's user error
  (same contract as not closing a file). Documented.
* **`time.ticker` without `:stop()`** — one entry survives for process lifetime,
  cancelled by `Drain` at process exit. Matches Go `time.Ticker` semantics.
* **`process.exec` watcher PIDs** — orthogonal; topology cleans them via
  `OnComplete`. No change.

## Out of scope but worth a separate audit

* `store.AddCleanup` usage outside time/ws (`excel`, `exec`, `sql`,
  `treesitter`, `cloudstorage`, `events`, `fs`, `security`) — most are
  disciplined (`cancelCleanup` stored and invoked), but early-error paths
  should be reviewed.
* `messageQueue` retention behaviour for non-ephemeral undelivered messages —
  what happens when a user-defined subscription never has a subscriber?

## Verification on production

After PR 5 lands, build and deploy to `100.70.10.25` (Tailscale host, runs
`memory-threads`). Observe over hours:

* Before: `runtime.MemStats.HeapInuse` for scheduler/job_worker processes grows
  monotonically.
* After: heap delta bounded by active workload (currently-armed timers).

If a diagnostic build is wanted before the fix lands, instrument
`subscribeContext.add` to log subscription count and topic prefix when
crossing thresholds. Topic prefix `after@*` / `ticker@*` / `ws@*` in the log
identifies the offender per service.

## Risk register

| Risk | Mitigation |
|---|---|
| `applyChannelResult` change disturbs existing well-behaved channel paths | Ship PR 1 standalone; full regression run before PR 3 |
| Router epoch + producer reverse map drift under load | I1+I2+I5 invariant tests + stress test with 10k concurrent ephemerals |
| Workflow processes use timers and break on replay | Gate router for workflow processes; do not migrate until replay design exists |
| Channel thread-safety violated by overlooked direct producer call | Single helper `Process.applyEphemeralFrame` for all router writes; grep for `Channel.TrySend`/`Channel.Send(nil,...)` outside step thread |
| Migration of ws producer breaks live connections | Migrate ws LAST; canary deploy; rollback plan = revert PR 6 |
| Pool cancellation skips Drain | `Abort` hook called on every executor early-exit path; covered by `TestPoolCancellation_DrainsEphemerals` |

## Status

* Design: locked at V4 after four rounds of hostile review (this branch).
* Tests: to be written before implementation, per PR.
* Implementation: not started.

## Decision log

| Decision | Why |
|---|---|
| No `__gc` finalizer | Project constraint. We control VM + flow; finalizers add nondeterminism. |
| No per-service `@time` topic | Doesn't scale to N services; hardcodes service lists into engine. |
| Single reserved `@pid/route` | One generic routing lane; new services add 3 lines of glue, runtime never grows. |
| No subs entry for the router | Lazy allocation; baseline overhead = one nullable pointer per process. |
| `OverflowClose` default for ws | Silent loss on ordered streams is worse than loud failure. |
| Epoch monotonic per process | Defends against process pool reuse + upgrade reuse. |
| Tombstones consumed by Start, not pure TTL | Late Start is the correctness path; TTL is memory hygiene only. |
| PR 1 ships standalone | Fixes an existing production bug independent of the leak. |
