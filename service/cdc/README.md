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
