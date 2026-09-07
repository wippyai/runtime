# SQLite driver and observation

Wippy pins `github.com/rqlite/go-sqlite3 v1.50.0` through a replacement of
`github.com/mattn/go-sqlite3`. The canonical import path is retained so the
runtime and extensions share one driver registration and SQLite implementation.
This is a SQLite driver dependency, not the rqlite server or Raft subsystem.

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

- Public SQL/Lua entry points are unchanged by the dependency replacement.
- Wippy still gates its observer on `sqlite_preupdate_hook`; ordinary builds
  expose SQL without that optional observation capability.
- The fork enables FTS5, DBSTAT, Session and pre-update support by default and
  omits shared cache. Shared-cache-dependent applications are not equivalent;
  private in-memory resources must retain one physical connection.
- The runtime does not create SQLite Sessions or change checkpoint policy.
- Native observation remains local to the observed connection pool, bounded,
  and non-durable. A driver replacement does not add replay or external-writer
  capture.
- Go module replacements are not inherited by embedding applications. A
  downstream custom runtime must carry the same replacement in its root
  go.mod to obtain the tested value contract.

## Verification

Run the SQL/CDC race suite and native SQLite integration suite:

```sh
go test -race -tags sqlite_preupdate_hook ./service/sql/... ./service/cdc/...
go test -race -tags 'integration sqlite_preupdate_hook' ./service/cdc/sqlite ./service/sql/engine/sqlite
go test -race -tags 'fts5 sqlite_vec sqlite_preupdate_hook' ./service/sql/... ./runtime/lua/modules/sql
go test ./service/sql/... ./service/cdc/sqlite ./runtime/lua/modules/sql
```

The extension test executes vector-distance, FTS5 and JSON queries through the
connector-owned driver. Type tests compare snapshots with both live row images.
The reducer model test checks 500 deterministic generated transaction histories.

`BenchmarkObserverCommit` measures plain-driver, idle-observer and subscribed
commit delivery separately. It is an in-memory microbenchmark, not disk or
multi-subscriber throughput evidence.
