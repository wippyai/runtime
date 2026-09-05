# Native SQLite CDC test

Build the runtime with `sqlite_preupdate_hook` (the normal Makefile build
includes it), then run from this directory:

```sh
../../dist/wippy-linux-amd64 test -s
```

The app uses a private in-memory database, no credentials or external services,
and explicit process policies granting `db.get` on its test database and
`cdc.source`/`cdc.subscribe` on its CDC source.
The runtime-registration check also grants test-only `registry.get` and
`registry.apply`; these administrative grants are not needed by normal readers.
It creates, updates and removes a second source while the first continues
receiving changes. Registration acceptance and readiness are tested separately.
It asserts committed live delivery, rowid zero, rollback isolation,
snapshot/live handoff, before images and explicit stream closure. Receive
operations have five-second deadlines; any assertion fails the command.

`cdc.stream()` constructs a lazy stream. Call `stream:channel()` to establish
the subscription before generating writes you expect it to observe. This is
live observation, not replay of changes made before subscription.

Byte-versus-string storage fidelity, savepoint reuse, coalescing and extension
compatibility are also covered by the Go tests in `service/sql/engine/sqlite`.
