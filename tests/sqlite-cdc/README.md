# Native SQLite CDC test

Build the runtime with `sqlite_preupdate_hook` (the normal Makefile build
includes it), then run from this directory:

```sh
../../dist/wippy-linux-amd64 test -s
```

The app uses a private in-memory database, no credentials or external services,
and an explicit process policy granting only `db.get` on its test database.
It asserts committed live delivery, rowid zero, rollback isolation,
snapshot/live handoff, before images and explicit stream closure. Receive
operations have five-second deadlines; any assertion fails the command.

`cdc.stream()` constructs a lazy stream. Call `stream:channel()` to establish
the subscription before generating writes you expect it to observe. This is
live observation, not replay of changes made before subscription.

Byte-versus-string storage fidelity, savepoint reuse, coalescing and extension
compatibility are also covered by the Go tests in `service/sql/engine/sqlite`.
