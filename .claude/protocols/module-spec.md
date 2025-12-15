# Module Specification Protocol v1

## Purpose

This protocol defines how to write Lua module specifications for agent consumption. Specs must enable an agent to correctly use a module without access to source code.

**Spec consumer:** Agent writing Lua 5.3 code using the module
**Protocol consumer:** Agent tasked with creating/updating a spec
**Goal:** 100% API coverage, zero ambiguity, maximum density

**Lua Environment:**
- Lua 5.3 with integer type and bitwise operators
- Sandboxed runtime - standard library is limited
- Many standard Lua functions may not be available
- Always verify what's actually exposed in the sandbox

---

## How This Protocol Is Used

This protocol is given to a child agent with a task like:

```
Create spec for module: {module_name}
Module path: runtime/lua/modules/{module_name}/
Protocol: protocols/module-spec.md
```

The child agent must:
1. Read and understand this entire protocol
2. Research the module thoroughly
3. Write the spec following this protocol exactly
4. Validate before outputting

---

## File Locations

```
runtime/lua/modules/{module}/
  module.go          # Main implementation (always exists)
  module_test.go     # Go tests (usually exists)
  yields.go          # Async operations (if module yields)
  errors.go          # Error definitions (if exists)
  spec.md            # Output location for spec
  *.go               # Other implementation files

app/src/test/{module}/
  _index.yaml        # Test registry
  *.lua              # Lua test files

runtime/lua/modules/
  README.md          # Module development guide
  errors.md          # Error handling reference
```

---

## Agent Execution Instructions

You are tasked with creating or updating a module specification. Follow these phases exactly.

### Phase 1: Research

Before writing anything, gather complete understanding of the module.

**Required reading:**

1. **Module implementation** (`module.go`)
   - Find all exported functions
   - Find all constants defined
   - Find all types/userdata created
   - Note error handling patterns (string vs structured)

2. **Yields file** (`yields.go` if exists)
   - Understand async operations
   - Find option structs and their fields
   - Note what operations yield/block

3. **Tests** (`module_test.go`, `*_test.go`)
   - Understand expected behavior
   - Find edge cases
   - Verify error conditions

4. **Integration tests** (`integration_test.go` if exists)
   - Real-world usage patterns
   - Complex scenarios

5. **App tests** (`app/src/test/{module}/` if exists)
   - Lua-side usage examples
   - Error handling patterns

6. **App folder** (`app/src/` broadly)
   - Search for real usage of this module across the app
   - Understand how it's used in practice, not just tests
   - Find patterns, common use cases, edge cases
   - This gives larger context than tests alone

7. **Existing spec** (`spec.md` if exists)
   - What's already documented
   - What might be outdated

**Build a mental inventory:**
- [ ] List of all functions
- [ ] List of all constants (with values)
- [ ] List of all types and their methods
- [ ] List of all error conditions
- [ ] List of all options with types and defaults

### Phase 2: Draft Spec

Write initial spec following the Document Structure below. Include everything from your inventory.

### Phase 3: Validate

Run the Validation Checklist against your draft. Fix all issues found.

### Phase 4: Density Pass

Remove any redundancy per the Anti-Patterns section.

### Phase 5: Self-Test

Read your spec as if you're a different agent who must use this module. Verify you could:
- Load the module correctly
- Call every function with correct arguments
- Access every constant with full path
- Handle every return value
- Handle every error condition

If anything is unclear, fix it.

### Phase 6: Output

Write the final spec to `{module_dir}/spec.md`.

### Phase 7: Update Status

Update `protocols/module-spec-status.md`:
1. Change module status to `draft`
2. Set "Last Updated" date
3. Add entry to Changelog

---

## Special Cases

### Core Modules (Globally Available)

Some modules are preloaded and don't need `require()`. Check for:
- `luaapi.ClassProcess` or similar core class
- Module loaded in engine initialization
- Tests that use module without require

**Globally available (no require, no Loading section needed):**
- `channel` - Message passing
- `errors` - Error constants and creation
- `payload` - Data transcoding
- `process` - Process management

For these, **omit the Loading section entirely** - agents don't need to require them.

### Engine-Level Specs

Core engine components (channel, coroutines, etc.) should be documented in a single engine spec at:
```
runtime/lua/engine/spec.md
```

This spec covers:
- Channels and their methods
- Coroutine/process primitives
- Other engine-level APIs available to all Lua code

These are not "modules" - they're engine primitives always available.

### Modules with Submodules

Some modules have nested tables (e.g., `crypto.random`, `text.regexp`). Document each submodule:

```markdown
## crypto.random

### bytes(length: integer) → string, error
...

## crypto.hmac

### sha256(key: string, data: string) → string, error
...
```

### Modules Returning Objects

When functions return userdata with methods:
1. Document the function that creates the object
2. Create a Types section for the object
3. List all methods on that type

### Modules with Channels

If module uses channels for async communication:
1. Document channel creation
2. Document receive pattern (`msg, ok = ch:receive()`)
3. Explain what `ok = false` means

### String vs Structured Errors

Determine error type by checking source:
- `lua.LString(err.Error())` → string errors
- `lua.NewLuaError()` or `lua.WrapErrorWithLua()` → structured errors

Document accordingly - agents must know whether to call `:kind()` or compare strings.

### Dependencies on Other Types

Modules often use types from other packages (DTOs, channels, etc.). When a module:
- Accepts a type from another module as parameter
- Returns a type from another module
- Uses channels for async communication

You MUST:
1. Document the dependency clearly
2. Include the type's relevant methods/fields in your spec
3. Reference the dependency spec if one exists

Example dependencies:
- `payload` - Used by many modules for data transcoding
- `channel` - Used for async message passing (spec at `runtime/lua/engine/`)
- `errors` - Structured error type (see `runtime/lua/modules/errors.md`)

Format for documenting dependencies:

```markdown
## Dependencies

### channel (from engine)

Used by `client:channel()` for receiving messages.

| Method | Signature | Returns |
|--------|-----------|---------|
| receive | () | value, ok: boolean |
| close | () | - |

See: `runtime/lua/engine/channel.go` or channel spec
```

If a dependency is complex, the agent should verify a spec exists for it. If not, flag it for creation.

---

## Document Structure

Specs follow this exact section order:

```
# {module_name}

{one-line description}. {classification tags}.

## Loading
## Constants (if any)
## Dependencies (if uses types from other modules/engine)
## Functions
## Types (if module has object types)
## Errors (if uses structured errors)
## Example
```

---

## Section: Header

First line after title. Contains:
1. One sentence description (what it does)
2. Classification tags from: `deterministic`, `nondeterministic`, `encoding`, `security`, `network`, `io`, `storage`, `process`, `time`, `workflow`

```markdown
# base64

Base64 encoding and decoding. Encoding, deterministic.
```

---

## Section: Loading

How to access the module.

**If requires loading:**
```markdown
## Loading

` ` `lua
local base64 = require("base64")
` ` `
```

**If globally available (core module):**
```markdown
## Loading

Global. No require needed.

` ` `lua
process.spawn(...)  -- direct access
` ` `
```

---

## Section: Constants

All constants with **full access paths** and values.

Format as code block showing actual usage:

```markdown
## Constants

` ` `lua
websocket.TYPE_TEXT                    -- "text"
websocket.TYPE_BINARY                  -- "binary"
websocket.TEXT                         -- 1
websocket.BINARY                       -- 2
websocket.COMPRESSION.DISABLED         -- 0
websocket.COMPRESSION.CONTEXT_TAKEOVER -- 1
websocket.CLOSE_CODES.NORMAL           -- 1000
websocket.CLOSE_CODES.GOING_AWAY       -- 1001
` ` `
```

**Rules:**
- Show EVERY constant from source
- Always show full path (`module.CONST`, not just `CONST`)
- Show actual value as comment
- Group related constants together

---

## Section: Functions

Each function gets:
1. Signature line
2. Parameters table (if has params)
3. Returns description
4. Errors list
5. Brief usage example (if non-obvious)

### Signature Format

```
### function_name(param: type, optional?: type) → ReturnType, error
```

**Type notation:**
| Notation | Meaning |
|----------|---------|
| `string` | String |
| `integer` | Integer (Lua 5.3) |
| `number` | Float |
| `boolean` | Boolean |
| `table` | Generic table |
| `any` | Any type |
| `T[]` | Array of T |
| `{[K]: V}` | Map with key type K, value type V |
| `{field: type, ...}` | Struct/record table |
| `T?` | Optional (may be nil) |
| `T \| U` | Union (either T or U) |
| `→` | Returns |

### Parameters Table

Required when function has parameters:

```markdown
| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| url | string | yes | - | WebSocket URL (ws:// or wss://) |
| options | table | no | nil | Connection options |
```

### Options Table

When a parameter is an options table, document its fields:

```markdown
**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| timeout | integer\|string | 0 | ms or Go duration ("5s") |
| retries | integer | 3 | max retry attempts |
```

### Returns

Explicitly state what is returned and when:

```markdown
**Returns:**
- Success: `encoded: string` - Base64 encoded result
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)
```

Or for simpler cases:
```markdown
**Returns:** `string, error` - encoded string or nil + structured error
```

### Errors

List error conditions with:
1. When it occurs
2. Error kind (if structured) or type (if string)

```markdown
**Errors (structured):**
| Condition | Kind | Retryable |
|-----------|------|-----------|
| input not string | errors.INVALID | no |
| malformed base64 | errors.INVALID | no |
```

Or for string errors:
```markdown
**Errors (strings):**
- `"URL required"` - url parameter empty
- `"connection refused"` - server unreachable
```

### Yields/Blocks

If function yields or blocks, note it:

```markdown
**Yields:** until connection established or timeout
```

---

## Section: Types

For modules that return objects/userdata with methods.

### Type Header

```markdown
## Types

### Client

Returned by `websocket.connect()`.
```

### Methods Table

```markdown
| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| send | (data: string, type?: integer) | - | type: websocket.TEXT or .BINARY |
| channel | () | Channel | first call subscribes |
| close | (code?: integer, reason?: string) | - | code default 1000 |
| ping | () | - | yields until sent |
```

### Method Details

For complex methods, add detail section:

```markdown
#### client:channel() → Channel

Returns channel for receiving messages.

- First call: subscribes to server, returns Channel
- Subsequent calls: returns same Channel

` ` `lua
local ch = client:channel()
` ` `
```

---

## Section: Errors

Only needed if module uses structured errors. Reference the error constants.

```markdown
## Errors

This module returns structured errors. Check kind with `errors.*` constants:

` ` `lua
local result, err = module.operation()
if err then
    if err:kind() == errors.INVALID then
        -- bad input
    elseif err:kind() == errors.NOT_FOUND then
        -- resource missing
    end
end
` ` `

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.INTERNAL`
```

---

## Section: Example

One complete, realistic usage example showing:
1. Loading
2. Primary operation
3. Error handling
4. Cleanup (if needed)

```markdown
## Example

` ` `lua
local websocket = require("websocket")

local client, err = websocket.connect("wss://echo.example.com", {
    dial_timeout = 5000
})
if err then error(err) end

client:send("hello")

local ch = client:channel()
while true do
    local msg, ok = ch:receive()
    if not ok then break end
    print(msg.type, msg.data)
end

client:close(websocket.CLOSE_CODES.NORMAL)
` ` `
```

---

## Validation Checklist

Run this checklist against every spec before finalizing.

### Layer 1: API Surface

Compare spec against module source code:

- [ ] Every exported function documented?
- [ ] Every constant documented with full path?
- [ ] Every object type documented?
- [ ] Every method on each type documented?
- [ ] No undocumented public API?

### Layer 2: Type Accuracy

- [ ] Every parameter has type annotation?
- [ ] Every return value has type annotation?
- [ ] Required vs optional clearly marked (? suffix)?
- [ ] Default values shown for all optionals?
- [ ] Union types shown where multiple types accepted?

### Layer 3: Behavior Coverage

- [ ] Success case documented for each function?
- [ ] All error conditions listed?
- [ ] Error type specified (string vs structured)?
- [ ] Yields/blocks noted where applicable?
- [ ] Edge cases documented (nil, empty, closed state)?

### Layer 4: Agent Simulation

Read spec as a dumb agent and verify you can:

- [ ] Determine how to load the module?
- [ ] Construct valid function calls with correct types?
- [ ] Access all constants with correct paths?
- [ ] Handle all possible return values?
- [ ] Handle all possible errors?
- [ ] Know when operations yield/block?
- [ ] Avoid common mistakes?

### Layer 5: Density Check

- [ ] No redundant examples (signature already clear)?
- [ ] No prose that could be a table?
- [ ] No implementation details (Go code, internals)?
- [ ] No "best practices" or "tips" sections?
- [ ] No "thread safety" boilerplate?

### Layer 6: Formatting Check

- [ ] Blank line before every markdown table?
- [ ] Code blocks have language hints (```lua)?
- [ ] No trailing whitespace?
- [ ] Consistent heading levels?

---

## Formatting Rules

- **Blank line before tables:** Always put a blank line before markdown tables, otherwise they won't render
- **Code blocks:** Use triple backticks with language hint (```lua)
- **No trailing whitespace**

---

## Anti-Patterns

**Never include:**

| Anti-pattern | Why |
|--------------|-----|
| Go source code | Internal detail, wastes tokens |
| Thread safety section | Always same boilerplate |
| Best practices | Agent can derive from API |
| Implementation notes | Internal detail |
| Multiple examples per function | Redundant if signature clear |
| Partial constant lists ("etc", "...") | Agent can't guess the rest |
| String paths without full qualifier | Agent will write `CONST` not `module.CONST` |
| Vague error descriptions | Agent won't handle correctly |

---

## Iteration Process

Specs should be refined through multiple passes:

### Pass 1: Draft
- Extract all API from source code
- Write initial spec following structure

### Pass 2: Completeness
- Run Layer 1-3 validation
- Add missing items

### Pass 3: Agent Test
- Run Layer 4 validation
- Fix ambiguities found

### Pass 4: Density & Formatting
- Run Layer 5-6 validation
- Remove redundancy
- Fix formatting issues

### Pass 5: Verification
- Test spec against actual module usage
- Verify all documented behavior matches reality

---

## Spec Maintenance

When module implementation changes:

1. Identify changed API surface
2. Update affected sections
3. Re-run validation checklist
4. Verify example still works

---

## Quick Reference Template

```markdown
# {module_name}

{description}. {classification}.

## Loading

` ` `lua
local mod = require("{module_name}")
` ` `

## Constants

` ` `lua
mod.CONST_NAME  -- value
` ` `

## Functions

### func(param: type, opt?: type) → Return, error

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|

**Returns:**

**Errors ({type}):**

## Types

### TypeName

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|

## Example

` ` `lua
` ` `
```
