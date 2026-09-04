# CDC ownership and runtime registration

CDC is a supervised source of committed database changes, not a general-purpose
message producer. Consumers subscribe through one driver-neutral Lua module.

| Layer | Responsibility |
| --- | --- |
| `api/service/cdc` | Source, stream, cursor, limits and configuration contracts |
| `system/cdc` | Canonical registry-ID lookup; no database implementation |
| `boot/components/service/storage/cdc.go` | Inject drivers and register entry listeners |
| `manager.go` | Route entry create/update/delete; supervise sources and own exclusive-resource leases |
| `slot.go` | Stable source identity and lifecycle; generation-stamp streams |
| `replacement.go` | Candidate activation and generation handoff |
| `retirement.go` | Retain failed cleanup and retry it without losing resource ownership |
| `dispatcher.go` | Own process subscriptions, bounded relay delivery and shutdown |
| `postgres`, `sqlite` | Database-specific capture, snapshots and recovery |
| `runtime/lua/modules/cdc` | Lua values, source permissions and process-owned stream cleanup |

The PostgreSQL implementation separates `service.go` (lifecycle),
`replication.go` (protocol and capture progress), `snapshot.go` (consistent read
and handoff), and `slots.go` (publication/slot administration). Admission belongs
to the common source slot, not either database implementation.

## Live delivery, not a durable queue

Changes are committed database observations delivered through Wippy's process
relay. There is no subscriber acknowledgment or durable subscriber inbox.
`capabilities.capture_resume` describes the source's ability to retain capture
progress; it does not promise delivery after a subscriber restart. `replayable`
is false for both current drivers. A persistent PostgreSQL slot can replay after
a server crash, so consumers must tolerate duplicates. Row delivery is not an
atomic transaction applied to the consumer.

Overflow terminates the affected subscription visibly. Resubscribe with a fresh
snapshot when rebuilding state. Work distribution, process groups and durable
jobs belong above this source API, not inside CDC.

PostgreSQL commits acknowledge the protocol's transaction-end LSN, never a byte
count derived from the logical wire encoding. Unchanged TOAST columns are absent
from `after` and listed in `unchanged`; a literal `"<unchanged-toast>"` is ordinary
text. Snapshots of publications containing row filters or column lists are
explicitly unsupported; live publication filtering remains PostgreSQL-owned.

## Bounded admission

Each source accepts a `subscriptions` configuration:

```yaml
subscriptions:
  max_subscriptions: 1024
  max_snapshots: 4
  max_bytes: 268435456
```

These are the defaults; negative values are rejected and zero selects the
default. `max_bytes` reserves the sum of requested driver-backlog limits, not
current queue fill. Snapshot-enabled streams retain their snapshot reservation
until closed, including their live phase. This deliberately conservative bound
also limits snapshot connections and temporary replication slots.

The default stream backlog is 1 MiB. A caller may request another `max_bytes`
within the source budget. Separately, the process dispatcher caps admission at
4096 streams and 1 GiB of logical reservations, counting both driver and delivery
backlogs. These bounds are not RSS guarantees: row maps, runtime bookkeeping,
database driver buffers and Lua values also consume memory.

`cdc.source(id).admission` reports `active`, `snapshots`, `reserved_bytes`, and
`rejected`. Limits update through normal registry replacement. Lowering a limit
does not revoke acquired streams; it rejects new admission until reservations
fit. Cleanup releases reservations on failed acquisition, explicit close and
natural stream termination.

## Installation and readiness

`db.cdc.postgres` and `db.cdc.sqlite` are ordinary registry entries. Runtime
registry changes invoke the same Add/Update/Delete listeners as boot. There is
no separate `cdc.register()` API. Registry mutation requires the normal
`registry.apply` permission (or the corresponding owned-overlay permission).
Driver implementations are injected at boot; installing a source at runtime
does not load arbitrary Go code or register a global SQL driver.

Registry acceptance is not service readiness. The supervisor starts accepted
services; inspect `cdc.source(id).state` before requiring a live subscription.
A failed/not-ready source must not silently look like an empty successful stream.
The native fixture in `tests/sqlite-cdc` exercises installation, readiness,
replacement, removal and concurrent delivery through the real boot path.

## Authority

- `cdc.source` on a source registry ID permits metadata inspection. Listings
  contain only permitted sources.
- `cdc.subscribe` on that ID permits reading its captured rows, including before
  images. It is checked at lazy stream creation and subscription acquisition.
- `db.get` does not imply either CDC permission, nor does CDC grant SQL access.
- Table/operation filters narrow delivery; they are not row-level authorization.
  Give narrower readers a separately configured source with a restricted table
  set. Existing acquired streams have their normal process-owned lifetime;
  policy edits do not retroactively revoke an already acquired channel.

These checks use Wippy's standard actor/scope and strict-mode rules. Trusted Go
drivers implement the internal contracts; Lua is the permission boundary.

## Multiple sources and recovery

Sources have independent registry identities and subscriber queues. Updating or
removing one must not stop unrelated sources. Multiple SQLite sources can observe
the same connector-owned database. Each declares its database as a supervisor
dependency, preserving startup and database-generation replacement ordering.
PostgreSQL sources need distinct exclusive
replication-slot identities; duplicate ownership is rejected, not raced.

Replacement keeps the public registry/supervisor object stable, swaps the driver
generation and closes old subscriptions. Consumers resubscribe; streams are not
silently spliced across generations. Failed cleanup remains owned and retryable.
Stop preserves durable PostgreSQL resources; explicit deletion may dispose them.

PostgreSQL checkpoints and SQLite process-local observation are not equivalent.
SQLite cannot resume a durable cursor: it returns unsupported and can establish
a new snapshot/live handoff instead. A slow subscriber fails within its bounded
budget; it cannot turn application commits into an unbounded delivery queue.

Production operations must monitor PostgreSQL retained WAL/slot health and
configure retention and failover prerequisites externally. Long SQLite snapshot
reads can delay WAL checkpointing; WAL databases require local storage, not a
network filesystem. SQLite CDC observes SQL through its connector, not external
writers or native incremental BLOB writes. Unsupported table forms fail closed.
