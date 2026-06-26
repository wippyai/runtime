# Design: `archive` — working with large zip/tar archives in Lua

## Context

Wippy Lua apps need to **read archives (zip first, then tar family) that arrive
as uploads or live in a named filesystem**, and to **build archives** (e.g. a
download `.zip`), all **without loading the whole archive into memory** — they
can be multiple GB. Today there is a `compress` module (gzip/deflate/zlib/
brotli/zstd, in-memory only) but **no archive (container) support** and no zip/
tar anywhere.

This is **not** a virtual-filesystem / PhysFS-style mount (an earlier draft went
that way — superseded). We don't mount archives as filesystems. We add a focused
**`archive` module**: open an archive over a byte source, iterate/stream/extract
its entries, and stream new entries into a writer. It interoperates with the
existing `fs`, `stream`, and `http` modules through their established handoff
conventions.

**This deliverable is the design/spec**, not the implementation — semantics are
pinned now so they don't drift.

**Confirmed scope:** both a **seekable** source (file in an fs / bytes → random
access) and a **forward-only stream** source (upload → sequential); the four
operations — **iterate+stream entries**, **open entry by name**, **extract to an
fs**, **create/write archives**; memory-bounded throughout.

> Pull note: produced from the current local `runtime/` checkout (plan mode
> blocked `git fetch`). Sync `runtime/` to remote HEAD and re-verify the cited
> paths before implementing.

---

## 1. Where it lives & what it reuses

- **New Lua module `archive`** under `runtime/runtime/lua/modules/archive/`,
  wired at boot exactly like `compress`
  (`boot/components/runtime/lua/compress.go` is the template). It is a sibling of
  `compress`, not part of `fs`.
- **Reuses existing seams (no new infra):**
  - Accepts sources via the same pattern `fs.write_file` already uses
    (`runtime/lua/modules/fs/fs.go`): `string` (bytes) │ a userdata that is an
    `io.Reader` │ a userdata that is a `resource.ReaderProvider`
    (`api/runtime/resource/context.go`) — which is how `stream.Stream` (uploads,
    `req:stream()`, multipart `file:stream()`) hands over its `io.Reader`.
  - Unwraps an `fs.File` userdata to the Go `fsapi.File` (`fs.File` + `io.Writer`
    + `io.Seeker` + `Sync()`; the directory backend's `*os.File` is also an
    `io.ReaderAt`, which is what zip random access needs) for seekable sources,
    and an `fs.FS` userdata to `fsapi.FS` for extract destinations
    (`ud.Value.(*fsmod.File)` / `(*fsmod.FS)`).
  - **Exposes each archive entry as a `stream.Stream`** via
    `streamsys.InsertWithSize(...)` (`system/stream/dispatcher.go`). Because
    `fs:writefile(path, <stream>)` already does `io.Copy(dstFile, reader)`,
    streaming extraction composes for free: `dest_fs:writefile(e.name, entry)`.
  - Lifecycle/error conventions: `value.RegisterTypeMethods`, `(result, error)`
    returns with `lua.NewLuaError(...).WithKind(...)`, and
    `resource.GetStore(ctx).AddCleanup(...)` + idempotent `:close()` (same as
    `fs.File`'s `NewFileWithCleanup`).
  - Codecs: stdlib `archive/zip`, `archive/tar`, `compress/gzip`, and
    `klauspost/compress/zstd` (already a dependency).

---

## 2. Lua SDK surface

### 2.1 Random reader — seekable source (full random access)

For a source that can seek (a file in an fs, or bytes). Zip's central directory
(at the end of the file) is read up front; entries decompress on demand.

```lua
-- Open by fs handle + path (the module opens the file, owns its lifecycle):
local r = archive.open(fs.get("app:uploads"), "incoming.zip")   -- reader, err
-- Or from an already-open seekable fs.File, or from raw bytes:
local r = archive.open(file)            -- file = fs:open("x.zip"), read off disk
-- bytes source holds the WHOLE archive in RAM (caller's allocation) — only for
-- small, already-in-memory archives. For large zips use an fs file or a stream,
-- never bytes:
local r = archive.open(zip_bytes, { format = "zip" })  -- small archives only

for e in r:entries() do                 -- iterate the directory (metadata only)
  print(e.name, e.size, e.is_dir)       -- e: {name,size,compressed_size,is_dir,mode,modified,method,crc32}
end

local info = r:stat("docs/readme.md")           -- entry info by name (metadata only)
-- read() returns the whole entry as a Lua string = RAM. Allowed ONLY for small
-- entries: it errors (kind=Invalid) above max_inline_bytes. For
-- anything large, stream it — never materialize a big entry:
local small = r:read("docs/readme.md")          -- ≤ max_inline_bytes, else error
local es    = r:stream("big.csv")               -- stream.Stream, decompresses on demand
while true do local c = es:read(64*1024); if not c then break end ... end
-- es is a real stream.Stream: registered in the stream system with an id, so it
-- composes everywhere a stream does — es:scanner("lines"), fs:writefile(p, es),
-- or hand it to another module (e.g. an HTTP response body) by value.

r:extract("docs/readme.md", fs.get("app:out"))  -- stream one entry → dest fs
r:extract_all(fs.get("app:out"), {              -- stream every entry → dest fs
  prefix = "job123/",   -- prepend to each dest path
  strip  = 1,           -- drop N leading path components
  filter = function(e) return not e.is_dir end,
})
r:close()
```

### 2.2 Sequential reader — forward-only stream (uploads)

For a source that cannot seek (an HTTP upload body / multipart file). Entries are
visited **in archive order**; each entry's reader is valid only until you advance.
No random `open(name)`.

```lua
local up = form.files.upload[1]:stream()        -- stream.Stream from multipart
local s  = archive.scan(up, { format = "zip" }) -- forward-only walker

for e, entry in s:walk() do                     -- entry is a stream.Stream
  if not e.is_dir then
    fs.get("app:uploads"):writefile("job123/"..e.name, entry)  -- streaming copy
  end
end
-- or, equivalently, stream everything out in one call:
-- s:extract_all(fs.get("app:uploads"), { prefix = "job123/" })
s:close()
```

**Format support for the sequential path (honest):** `tar`, `tar.gz`, `tar.zst`
stream natively (Go's `tar.NewReader` over a (decompressed) `io.Reader`). **zip**
is parsed via its per-entry **local headers** sequentially; entries written with
a streaming *data descriptor* (size/CRC trailing the data, ZIP flag bit 3) are
read by decompressing to the entry boundary, and a handful of zip edge cases
(some zip64 / encrypted variants) are not streamable. **Recommended for zip
uploads:** the *stage-then-random* pattern below, which is fully streaming, O(1)
memory, and gives robust random access.

### 2.3 Stage-then-random (recommended for zip uploads)

You already have a filesystem; landing the upload as a file first is a bounded
sequential copy (never a RAM load) and then you get the rock-solid random reader:

```lua
local dst = fs.get("app:tmp")
dst:writefile("u.zip", req:stream())            -- streaming copy upload → fs file
local r = archive.open(dst, "u.zip")            -- robust random access
... -- entries / read / extract_all
r:close(); dst:remove("u.zip")
```

### 2.4 Writer — create/stream archives

Builds an archive by streaming entries into any writer. The destination can be a
file in an fs **or** a writable `stream.Stream` (e.g. an HTTP response), so a
download `.zip` is generated straight to the wire with bounded memory.

```lua
local w = archive.create(fs.get("app:tmp"), "out.zip", { format = "zip" })
-- or stream to a response: archive.create(res:stream(), { format = "zip" })

w:add("notes.txt", "hello")                        -- from a string/bytes/reader
w:add_file("data/big.bin", fs.get("app:data"), "big.bin")  -- streamed from fs
w:add("from_upload", some_stream)                  -- any reader/stream.Stream
w:add_dir("empty/")
w:close()                                          -- writes the central directory
```

`add*` options: `{ method = "store"|"deflate", level, mode, modified }`. The zip
writer streams to non-seekable writers using data descriptors, so writing to a
response stream works.

### 2.5 `opts` (all optional)

| Key | Default | Meaning |
| --- | --- | --- |
| `format` | auto | `"zip"`,`"tar"`,`"tar.gz"`,`"tar.zst"`; auto = sniff magic, else extension. |
| `max_entries` | 100_000 | Reject archives with more entries (bomb defense). |
| `max_total_bytes` | 2 GiB | Cap on cumulative uncompressed output during read/extract (work limit). |
| `max_file_bytes` | 1 GiB | Cap on a single entry's uncompressed size (work limit). |
| `max_inline_bytes` | 16 MiB | Hard cap for the **RAM-materializing** calls `read()`/`read(name)`; above it they error and you must use `stream()`/`extract()`. |
| `buffer_bytes` | 64 KiB | Streaming copy buffer for read/extract/add. |

Limits are enforced **during** read/extract (lazily), so listing entries never
forces decompression. **`max_total_bytes`/`max_file_bytes` are work caps, not RAM
caps** — streaming an entry never holds more than `buffer_bytes` + the codec's
decompression window. The only RAM-sizing knob is `max_inline_bytes` (and the
caller's own bytes source).

---

## 3. Go extensibility — codec registry (add a format natively)

Adding a format must not touch the Lua API or the module core. New file
`api/fs/archive.go` (or a dedicated `api/archive` package):

```go
type Codec interface {
    Name() string             // "zip","tar","tar.gz","tar.zst"
    Extensions() []string
    Sniff(header []byte) bool  // magic-byte detection
}

// Capabilities a codec opts into — each is independent.
type RandomReadable interface { Codec; OpenRandom(r io.ReaderAt, size int64, o Options) (Reader, error) }
type StreamReadable interface { Codec; OpenStream(r io.Reader, o Options) (Walker, error) }
type Writable       interface { Codec; OpenWriter(w io.Writer, o Options) (Writer, error) }

type Reader interface {           // random access over a seekable source
    Entries() []Entry
    Open(name string) (io.ReadCloser, *Entry, error)
    Close() error
}
type Walker interface {           // forward-only
    Next() (*Entry, io.Reader, error)  // reader valid until the next Next()
    Close() error
}
type Writer interface {
    Create(e Entry) (io.Writer, error)
    Close() error
}
```

Detection order: explicit `opts.format` → `Sniff` (first ~512 bytes) → extension
→ `UnknownFormat` (`kind = Invalid`). Built-ins register via `init()`.

### Built-in codec capabilities (v1)

| id | RandomReadable | StreamReadable | Writable |
| --- | --- | --- | --- |
| `zip` | yes (`zip.NewReader`) | yes (local-header parse, §2.2 caveats) | yes (`zip.Writer`, data descriptors) |
| `tar` | yes (build offset index over the seekable source) | yes (`tar.NewReader`) | yes (`tar.Writer`) |
| `tar.gz`/`tgz` | future (zran checkpoint index) | yes (gzip then tar) | yes (tar then gzip) |
| `tar.zst` | future (zstd seekable frames) | yes (zstd then tar) | yes (tar then zstd) |

`archive.open` (random) requires a `RandomReadable` codec; `archive.scan`
(sequential) requires a `StreamReadable` codec; otherwise `kind = Unavailable`
with a message pointing at the other entrypoint (or stage-then-random).

Adding `7z`/`cpio`/etc. later = implement the interfaces you can support and
register — no module or Lua-API change.

---

## 4. Memory & safety (the whole point: large archives)

### 4.1 Memory guarantee (low-RAM server, multi-GB archives — no OOM)

**Hard invariant: peak resident memory is independent of archive size and of any
single entry's size.** A 50 GB zip on a 512 MB server must work. Concretely, the
runtime never holds more than:

- the codec **decompression window** (deflate ≈ 32 KB; zstd window per level), plus
- one **`buffer_bytes`** copy buffer (default 64 KB) per active entry, plus
- the **per-entry metadata being iterated** — and `r:entries()` / `s:walk()` are
  **lazy iterators** that yield one entry at a time; they never build a Lua table
  of all entries. (Zip's central directory is read incrementally; even at the
  `max_entries` ceiling the metadata is names+offsets, bounded and small.)

Mechanically: random zip reads only the central directory up front and
decompresses each entry on demand into a streaming reader; sequential reads pull
bounded buffers; `extract`/`extract_all`/`add*` use `io.CopyBuffer(buffer_bytes)`
straight between the entry and the fs file/stream. **The archive is never loaded
into RAM and never extracted to a scratch copy to be read.**

**The only two ways a caller can consume archive-sized RAM — both guarded:**

| Footgun | Guard |
| --- | --- |
| `archive.open(bytes, …)` — a bytes source is the whole archive in RAM | Documented as small-archives-only; large archives use an fs file (read off disk) or a stream. |
| `read()` / `read(name)` — materializes one entry as a Lua string | Hard-errors above `max_inline_bytes` (16 MiB); large entries **must** use `stream()`/`extract()`. |

Everything else — listing, streaming reads, extract-to-fs, create — is O(window +
buffer) regardless of how big the archive or its entries are.
### 4.2 Safety

- **Decompression-bomb defense** (mandatory): enforce `max_entries`,
  `max_total_bytes`, `max_file_bytes` during read/extract →
  `kind = Invalid`. These cap *output work*, not a backing buffer
  (nothing is buffered whole).
- **Zip-slip / path traversal** (mandatory): on extract, sanitize every entry
  name — reject `..` segments, absolute paths, and Windows drive/UNC prefixes;
  an entry resolving outside the destination root is dropped with a logged
  warning, never written.
- **Lifecycle:** readers/writers/entry-streams register cleanup with
  `resource.GetStore(ctx)` and auto-close at task scope; explicit `:close()` is
  idempotent. A streamed entry from §2.2 is invalidated when the walk advances —
  reading a stale entry returns `kind = Internal`.

### Error taxonomy

Kinds are the runtime's existing Lua error kinds (`lua.NewLuaError(...).WithKind(...)`
— `Invalid`, `NotFound`, `PermissionDenied`, `Internal`, `Unavailable`, etc.); the
`(detail)` tag is a stable sub-reason carried in the message, not a new kind.

| Condition | Kind (detail) |
| --- | --- |
| Unknown / forced-but-mismatched format | `Invalid` (`unknown_format`) |
| Corrupt / truncated archive | `Invalid` (`corrupt_archive`) |
| `open(name)` on a sequential source, or stream-only format on `archive.open` | `Unavailable` (`random_access_unavailable`) |
| Limit exceeded (entries / total / file / inline size) | `Invalid` (`limit_exceeded`) |
| Source not readable / dest not writable | `PermissionDenied` |
| Entry name not found | `NotFound` |
| Read a stale streamed entry after walk advanced | `Internal` |

---

## 5. Security

- Gate the entrypoints with `security.IsAllowed(ctx, "archive.read", name, nil)`
  / `"archive.write"`, mirroring how `fs.get` gates `"fs.get"`. Sources/dest that
  come from an `fs` handle already passed `fs.get`'s check.
- Apply the zip-slip sanitization above to **all** extract destinations.

---

## 6. Explicitly out of scope (v1)

- Random access into compressed-tar (`tar.gz`/`tar.zst`) — sequential only for
  now; random is a future zran-style checkpoint index (RAM-bounded, disk-free).
- Encrypted / password archives; `7z`, `rar`, `cpio`, `ar`.
- Editing an existing archive in place (use `archive.create` to write a new one).
- Mounting archives as a filesystem (the superseded PhysFS-style approach).

---

## 7. Files when implemented (reference, not built now)

| Purpose | Path |
| --- | --- |
| Codec contracts + registry | `api/archive/` (new) or `api/fs/archive.go` |
| zip / tar / tar.gz / tar.zst codecs | `system/archive/` (new pkg) |
| Lua `archive` module (open/scan/create) | `runtime/runtime/lua/modules/archive/` (new) |
| Boot wiring | `boot/components/runtime/lua/archive.go` (+ constant) |
| Reuse: entry-as-stream | `system/stream/dispatcher.go` (`InsertWithSize`) |
| Reuse: source handoff | `resource.ReaderProvider` (`api/runtime/resource/context.go`) |
| Reuse: fs unwrap | `runtime/lua/modules/fs/fs.go` (`*FS`, `*File`) |
| Reuse: cleanup pattern | `resource.GetStore` (as in `fs/file.go`) |
| Spec (this doc, in-repo) | `runtime/runtime/lua/modules/archive/spec.md` |

## 8. Verification (for the implementation phase)

1. Build: from `runtime/`, `make build-wippy` (CGO, tags `fts5 sqlite_vec treesitter`).
2. Go unit tests in `system/archive`: zip/tar/tar.gz/tar.zst round-trips; random
   `Open(name)`; sequential `walk`; limit enforcement; zip-slip rejection;
   data-descriptor streaming zip; bounded-memory assertion (read a multi-GB
   fixture under a capped heap).
3. Lua integration test under `tests/modules/`: open from an fs file and from a
   `stream.Stream`; `entries`/`read`/`stream`/`extract_all`; `archive.create`
   round-trip to an fs file and to a writable stream; plus `make test-runtime`.
4. `make lint` (golangci-lint v2.8.0) and `make mutation MUTATE_DIR=system/archive`.
