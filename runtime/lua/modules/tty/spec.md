<!-- SPDX-License-Identifier: MPL-2.0 -->

# tty

Terminal input, styled text, physical presentation surfaces, and local virtual
viewports. IO, process-scoped, nondeterministic.

```lua
local tty = require("tty")
```

## Model

A `Surface` is one process's exclusive presentation lease on its terminal port.
It publishes complete row snapshots; the backend owns diffing and terminal
recovery. A `Canvas` is an in-process styled-cell composition buffer. A
`Viewport` is a local, structured terminal boundary that lets one process host
another process's surface without sharing byte streams or scheduler internals.

The shell decides where viewport content appears and translates input into the
child's one-based coordinates. The child sees an ordinary `tty` port and does
not know whether it is full-screen, tiled, tabbed, or hidden.

Viewports are local to one runtime node. They are opaque capabilities, not
serializable network references. A remote transport can implement the same Go
port contracts, but this module does not make local handles remotely valid.

## Module functions

| Function | Returns | Purpose |
|---|---|---|
| `start()` | `boolean, error?` | Start input delivery for the current port |
| `stop()` | `boolean, error?` | Stop input delivery |
| `screen_size()` | `width, height, error?` | Read current terminal dimensions |
| `events()` | `EventChannel, error?` | Subscribe once to canonical TTY events |
| `mouse(enable)` | `boolean, error?` | Enable or disable mouse reporting |
| `surface(options?)` | `Surface, error?` | Acquire the port's presentation lease |
| `canvas(width, height)` | `Canvas` | Allocate a bounded styled-cell buffer |
| `viewport(options?)` | `Viewport, error?` | Create a local virtual viewport |
| `attach(handle)` | `Viewport, error?` | Attach another viewer to a local viewport |
| `style()` | `Style` | Create a chainable text style |
| `bind(config)` | `KeyBinding` | Create a key matcher |

Call `events()` before or near `start()` so an interactive producer is ready to
consume input. On virtual ports, `start()` opens viewer-to-producer event
delivery and `stop()` closes it; `Viewport:send()` outside that interval fails
instead of silently losing input. Resize is independent of input state.

## Events

Every event is a record discriminated by `type`:

| Type | Fields |
|---|---|
| `key` | `key`, `key_type`, `action`, `alt`, `ctrl`, `shift` |
| `mouse` | `action`, `button`, `x`, `y`, `alt`, `ctrl`, `shift` |
| `resize` | `width`, `height` |
| `start` | `width`, `height` |
| `focus` | `focused` |
| `visibility` | `visible` |
| `paste` | `text` |
| `close` | none |

Coordinates are one-based. Focus reports keyboard ownership; visibility only
reports whether repainting is useful. Neither field prescribes application
lifecycle or background computation.

## Surface

```lua
local surface = assert(tty.surface({
    alternate_screen = true,
    hide_cursor = true,
    synchronized_output = true,
}))
```

Only one surface may be open on a port. `close()` is idempotent and releases the
lease; physical backends restore terminal modes. Garbage collection is a safety
net, not the preferred ownership boundary.

### `surface:present(rows, options?) -> stats, error?`

Publishes a complete array of row strings. `options.cursor` has one-based `x`
and `y` plus `visible`. Omitting the cursor preserves the last explicit cursor
state. The returned immutable stats record contains `rows`, `changed_rows`, and
`bytes_written`. An unchanged physical frame performs no write.

### `surface:invalidate() -> boolean`

Forgets backend presentation state without erasing the logical frame. The next
present commits even when its rows are unchanged. Use this after an outer
terminal resize or another owner may have disturbed physical state.

### `surface:close() -> boolean, error?`

Releases the surface exactly once and returns the first close result on later
calls.

## Canvas

`tty.canvas(width, height)` creates a bounded ANSI-aware cell buffer. It clips
at cell boundaries, understands grapheme width and styles, and prevents clipped
escape sequences from leaking into neighboring content.

Drawing accepts styled text, not terminal commands. SGR colors and OSC 8 links
are preserved; terminal erase, cursor-motion, and other control-only output is
not emitted. Each placement is clipped independently. A newline ends a row;
use `put_rows` for multiple rows and paint shell borders separately from child
content.

| Method | Purpose |
|---|---|
| `clear(fill?)` | Clear all cells, optionally repeating a styled fill |
| `put(x, y, text, width?)` | Place and clip one styled row |
| `put_rows(x, y, rows, width?)` | Place multiple styled rows atomically after validation |
| `rows()` | Render the complete row array for a surface |

Canvas coordinates are one-based but may be negative for clipping. Canvas area
is capped at 1,048,576 cells.

## Viewport

```lua
local view = assert(tty.viewport({width = 80, height = 24}))
local child = assert(process.with_options({terminal = assert(view:grant())})
    :spawn("app:child", "app:workers"))
```

`grant()` returns the one-shot producer capability. Admission consumes it
transactionally: a rejected process start restores an unresolved grant, while
a process that has resolved the port consumes it permanently. Unsupported
hosts reject terminal attachments rather than dropping them.

`handle()` returns a viewer capability. `tty.attach(handle)` adds another local
viewer. Handles do not grant presentation ownership and do not cross nodes.

| Method | Purpose |
|---|---|
| `grant()` | Return the creator's one-shot producer grant |
| `handle()` | Return the local viewer handle |
| `snapshot(after_revision?)` | Read current dimensions, rows, cursor, and revision; return `nil` when unchanged |
| `updates()` | Return a channel of coalesced revision watermarks |
| `send(event)` | Forward validated input to a started producer |
| `resize(width, height)` | Update geometry and notify producer/viewers when changed |
| `close()` | Detach only this viewer |

Updates are bounded hints, not an event log. A slow viewer receives the newest
available watermark and must call `snapshot()`. Presentation and resize never
block on slow viewers. Closing the last viewer does not kill a live producer;
closing the producer port does not destroy state while viewers remain.

## Minimal composite example

```lua
local events = assert(tty.events())
assert(tty.start())
local output = assert(tty.surface({alternate_screen = true, hide_cursor = true}))
local width, height = tty.screen_size()
local child = assert(tty.viewport({width = width, height = height - 1}))
local updates = assert(child:updates())

-- Pass child:grant() through process options when spawning the producer.
-- The shell can place child:snapshot().rows into any canvas region.

assert(output:close())
assert(child:close())
assert(tty.stop())
```

For a byte-oriented PTY such as a shell, Codex, or Claude Code, allocate it with
`exec` and call `process:attach_terminal()`. That adapter owns PTY emulation,
resize, input encoding, graceful termination, forced termination, and reaping;
the enclosing surface/viewport model stays the same for native and Docker
executors.
