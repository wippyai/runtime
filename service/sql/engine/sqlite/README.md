# SQLite driver and observation

Wippy imports `github.com/rqlite/go-sqlite3` directly; `go.mod` pins the tested
version. SQL pools and registry history share physical connection initialization
through `internal/sqliteconn`, without replacing the global driver registration.
This is a SQLite driver dependency, not the rqlite server or Raft subsystem.

## WAL checkpoints and durability

Every physical connection uses `wal_autocheckpoint=1000`, including replacement
connections and read-only CDC snapshot connections. This retains SQLite's
ordinary checkpoint policy even though the rqlite fork disables it by default
for its own Raft snapshot machinery. Automatic checkpoints use PASSIVE mode:
they do not wait for readers and do not replace commit, rollback or pre-update
hooks. The runtime does not add a checkpoint worker or truncate WAL on each write.

The 1000-page threshold is a trigger, not a WAL size limit. Long-lived readers
and large transactions can grow the WAL; checkpointing and recycling resume
when readers release their snapshots. Recycling reuses the WAL's allocated space
and does not necessarily shrink the file. Ordinary close-time cleanup is retained.

Synchronization is unchanged. Application SQLite and registry history use the
driver's default `synchronous=NORMAL` in WAL mode: commits survive a process
crash, but recent acknowledged commits can be lost after a power loss or OS
crash. `synchronous=FULL` is a separate durability policy with a sync cost per
commit; the initializer preserves explicit DSN synchronization settings.

See [SQLite WAL](https://www.sqlite.org/wal.html) and
[synchronous](https://www.sqlite.org/pragma.html#pragma_synchronous) for the
storage guarantees and checkpoint behavior.

## Foreign keys

Set `foreign_keys: true` on a `db.sql.sqlite` entry to enforce foreign key
constraints, including `ON DELETE CASCADE`, on every physical connection.
The option defaults to `false` for compatibility with existing databases.
It works with file databases and `file: ":memory:"`; enforcement is a SQLite
connection setting and does not change other clients that open the same file.

## Value contract

The fork preserves SQLite storage types in pre-update row images: TEXT is a
Go string, BLOB is bytes, INTEGER is int64, REAL is float64, and NULL is nil.
Text conversion is length-aware, including embedded NULs. Column affinity and
UTF-8 validity must not be used to guess a value's type.

Physical rowids are private to capture. Every signed rowid, including zero,
is valid. Coalescing emits initial/final images per physical row address in
first-touch order. Moving a rowid removes the old address and adds the new one;
reusing an address cannot alias a different surviving row. This is net-state
observation, not an audit trail of every statement.

## Compatibility boundaries

- Public SQL/Lua entry points are unchanged by the driver update.
- Wippy still gates its observer on `sqlite_preupdate_hook`; ordinary builds
  expose SQL without that optional observation capability.
- The fork enables FTS5, DBSTAT, Session and pre-update support by default and
  omits shared cache. Shared-cache-dependent applications are not equivalent;
  private in-memory resources must retain one physical connection.
- The runtime does not create SQLite Sessions. Explicit connection initialization
  preserves the checkpoint policy used before the v1.53 driver update.
- Native observation remains local to the observed connection pool, bounded,
  and non-durable. A driver replacement does not add replay or external-writer
  capture.
- Downstream custom runtimes use the same rqlite import and shared initializer
  to retain the tested value and connection contracts.

## Verification

Run the SQL/CDC race suite and native SQLite integration suite:

```sh
go test -race -tags sqlite_preupdate_hook ./service/sql/... ./service/cdc/...
go test -race -tags 'integration sqlite_preupdate_hook' ./service/cdc/sqlite ./service/sql/engine/sqlite
go test -race -tags 'fts5 sqlite_vec sqlite_preupdate_hook' ./service/sql/... ./runtime/lua/modules/sql
go test ./service/sql/... ./service/cdc/sqlite ./runtime/lua/modules/sql
go test -race ./internal/sqliteconn ./system/registry/history/sqlite
```

The extension test executes vector-distance, FTS5 and JSON queries through the
connector-owned driver. Type tests compare snapshots with both live row images.
The reducer model test checks 500 deterministic generated transaction histories.

`BenchmarkObserverCommit` measures plain-driver, idle-observer and subscribed
commit delivery separately. It is an in-memory microbenchmark, not disk or
multi-subscriber throughput evidence.

Checkpoint regressions cover physical connection replacement, controlled WAL
growth before close, unchanged synchronization, read-only opens, and progress
after a pinned snapshot releases its WAL frames. CI runs the SQL/history tests
with and without observation on Linux, macOS and Windows.
