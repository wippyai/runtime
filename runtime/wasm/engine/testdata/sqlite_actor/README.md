# SQLite stateful actor fixture

Heavy in-memory SQLite workload guest. `run` owns one SQLite database across
`receive`/`send` suspensions. SQL is upstream SQLite 3.53.2 (rusqlite bundled
amalgamation), not a stub. Asyncify is the host runtime's embedded
instrumentation; this binary is not pre-instrumented with `wasm-opt --asyncify`.

WIT is an exact copy of `../actor/actor.wit` (`wippy:actor@0.1.0`).

## Protocol

Incoming messages use the topic string only. Payload is ignored.

| Topic | Result topic `result` text | Notes |
| --- | --- | --- |
| `load:N` | `loaded:N` | Drop/reopen `:memory:` DB. Create `docs(id INTEGER PRIMARY KEY, value INTEGER, body TEXT)`, insert ids `0..N-1` in one transaction (`value=id*3`, body `document {id} group {id%100} indexing workload`), then `CREATE INDEX docs_value_idx ON docs(value)`. `N` is `0..=1000000`. |
| `get:ID` | `value:ID:VALUE` | Prepared `SELECT value FROM docs WHERE id = ?`. |
| `put:ID:VALUE` | `updated:ID:VALUE` | Prepared `UPDATE` of an existing row only. |
| `sum` | `sum:COUNT:SUMVALUE` | Prepared `SELECT COUNT(*), COALESCE(SUM(value), 0) FROM docs`. |
| `stats` | `stats:USED:HIGHWATER:PAGE_COUNT:PAGE_SIZE` | `sqlite3_memory_used`, `sqlite3_memory_highwater(0)`, `PRAGMA page_count`, `PRAGMA page_size`. |
| `stop` | (no reply) | Close the DB and return `Ok` from `run`. |

Protocol errors reply on topic `error` with a descriptive text payload. The guest does not panic on bad topics, missing rows, overflow, or SQLite errors.

## Pinned versions

| Item | Version |
| --- | --- |
| rustc | 1.96.0 (ac68faa20 2026-05-25) |
| target | `wasm32-wasip2` |
| wit-bindgen | 0.58.0 |
| rusqlite | 0.40.2 (`bundled`, `cache`) |
| libsqlite3-sys | 0.38.2 (bundled amalgamation) |
| SQLite | 3.53.2 / `SQLITE_VERSION_NUMBER` 3053002 / `SQLITE_SOURCE_ID` `2026-06-03 19:12:13 d6e03d8c777cfa2d35e3b60d8ec3e0187f3e9f99d8e2ee9cac695fd6fcdf1a24` |
| cc | 1.4.5 |
| WASI SDK | 34.0 (`clang` 23.1.0-wasi-sdk, wasi-libc `2e6fb9d8ee0c`) unpacked at `/tmp/wasi-sdk-34.0-x86_64-linux` |

## SQLite build flags

Set via `.cargo/config.toml` `LIBSQLITE3_FLAGS` (applied after rusqlite bundled defaults). wasm32-wasip2 also gets rusqlite's `SQLITE_THREADSAFE=0`.

```
SQLITE_OS_OTHER=1
SQLITE_TEMP_STORE=3
SQLITE_OMIT_LOAD_EXTENSION
SQLITE_OMIT_WAL
SQLITE_OMIT_DEPRECATED
SQLITE_OMIT_PROGRESS_CALLBACK
SQLITE_OMIT_SHARED_CACHE
SQLITE_OMIT_UTF16
SQLITE_OMIT_COMPLETE
SQLITE_OMIT_AUTHORIZATION
SQLITE_OMIT_GET_TABLE
SQLITE_OMIT_INCRBLOB
SQLITE_OMIT_DATETIME_FUNCS
SQLITE_OMIT_JSON
SQLITE_OMIT_TRACE
SQLITE_OMIT_TCL_VARIABLE
SQLITE_DISABLE_LFS
SQLITE_DQS=0
SQLITE_DEFAULT_MEMSTATUS=1
SQLITE_DEFAULT_FOREIGN_KEYS=0
-USQLITE_ENABLE_FTS3
-USQLITE_ENABLE_FTS3_PARENTHESIS
-USQLITE_ENABLE_FTS5
-USQLITE_ENABLE_RTREE
-USQLITE_ENABLE_DBSTAT_VTAB
-USQLITE_ENABLE_STAT4
-USQLITE_ENABLE_COLUMN_METADATA
-USQLITE_ENABLE_LOAD_EXTENSION
-USQLITE_SOUNDEX
-UHAVE_USLEEP
-UHAVE_LOCALTIME_R
-U_POSIX_THREAD_SAFE_FUNCTIONS
-U_WASI_EMULATED_MMAN
-U_WASI_EMULATED_GETPID
-U_WASI_EMULATED_SIGNAL
-U_WASI_EMULATED_PROCESS_CLOCKS
```

`src/memvfs.c` is a memory-only `SQLITE_OS_OTHER` VFS (`zName=mem`) so SQLite C does not call POSIX file APIs. The guest opens with `sqlite3_open_v2(":memory:", ..., "mem")`.

Observed compile options in the artifact: `THREADSAFE=0`, `TEMP_STORE=3`, `OMIT_WAL`, `OMIT_LOAD_EXTENSION`, `COMPILER=clang-23.1.0`.

## Artifact

`runtime/wasm/engine/testdata/sqlite_actor.wasm`

- SHA-256 `a3f0e280a029d4c4d0548b9ef7322a551c24c0b9b0fd79833726e17c92d29de6`
- 1186551 bytes
- WebAssembly component (version 13)
- `wasm-tools validate` succeeds
- no `asyncify` bytes

Component imports `wippy:actor/process@0.1.0` (`send`, `receive`) plus rustc `wasm32-wasip2` libstd WASI Preview 2 adapters (`wasi:cli@0.2.6`, `wasi:io@0.2.6`, `wasi:clocks/wall-clock@0.2.6`, `wasi:filesystem@0.2.6`). Those WASI imports are libstd, not SQLite disk I/O. Instantiation needs the actor host plus the engine's default WASI profiles.

## Rebuild

From the runtime repository root. WASI SDK must be at `$WASI_SDK_PATH` (download `wasi-sdk-34.0-x86_64-linux.tar.gz` into `/tmp` if needed).

```sh
export WASI_SDK_PATH=/tmp/wasi-sdk-34.0-x86_64-linux
export CC_wasm32_wasip2="$WASI_SDK_PATH/bin/clang"
export AR_wasm32_wasip2="$WASI_SDK_PATH/bin/llvm-ar"
export CFLAGS_wasm32_wasip2="--target=wasm32-wasip2 --sysroot=$WASI_SDK_PATH/share/wasi-sysroot -fPIC -O2"
CARGO_TARGET_DIR=/tmp/w1-sqlite-actor-target cargo test --locked --manifest-path runtime/wasm/engine/testdata/sqlite_actor/Cargo.toml
CARGO_TARGET_DIR=/tmp/w1-sqlite-actor-target cargo build --locked --release --target wasm32-wasip2 --manifest-path runtime/wasm/engine/testdata/sqlite_actor/Cargo.toml
cp /tmp/w1-sqlite-actor-target/wasm32-wasip2/release/sqlite_actor_fixture.wasm runtime/wasm/engine/testdata/sqlite_actor.wasm
wasm-tools validate runtime/wasm/engine/testdata/sqlite_actor.wasm
```
