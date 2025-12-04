# Lua Module Development Guide

## Module Structure

Each module lives in its own package under `runtime/lua/modules/`:

```
runtime/lua/modules/
  mymodule/
    module.go       # Module implementation
    module_test.go  # Go tests
    spec.md         # API specification
```

App tests live in `app/src/test/`:

```
app/src/test/
  mymodule/
    _index.yaml     # Test registry
    encode.lua      # Test file
    decode.lua      # Test file
    errors.lua      # Error handling tests
```

## ModuleDef Format

All modules use `luaapi.ModuleDef`:

```go
package mymodule

import (
    luaapi "github.com/wippyai/runtime/api/runtime/lua"
    lua "github.com/yuin/gopher-lua"
)

var Module = &luaapi.ModuleDef{
    Name:        "mymodule",
    Description: "Description of what module does",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
    Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
    mod := lua.CreateTable(0, 2)  // REQUIRED: use CreateTable with exact size
    mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
    mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
    mod.Immutable = true
    return mod, nil
}
```

### Class Constants

Use constants from `luaapi`:

- `luaapi.ClassDeterministic` - Same input = same output
- `luaapi.ClassNondeterministic` - Output varies (time, random)
- `luaapi.ClassEncoding` - Data serialization
- `luaapi.ClassSecurity` - Security operations
- `luaapi.ClassIO` - External I/O operations
- `luaapi.ClassNetwork` - Network operations
- `luaapi.ClassTime` - Time-related
- `luaapi.ClassProcess` - Process management
- `luaapi.ClassStorage` - Data storage
- `luaapi.ClassWorkflow` - Workflow-safe replacements

## Structured Errors

All errors must be structured using `lua.LuaError`. Never return plain `lua.LString`.

### Error Helpers

```go
// For invalid input errors
func invalidError(l *lua.LState, msg string) int {
    err := lua.NewLuaError(l, msg).
        WithKind(lua.KindInvalid).
        WithRetryable(false)
    l.Push(lua.LNil)
    l.Push(err)
    return 2
}

// For internal/Go errors
func internalError(l *lua.LState, goErr error, context string) int {
    err := lua.WrapErrorWithLua(l, goErr, context).
        WithKind(lua.KindInternal).
        WithRetryable(false)
    lua.SetErrorMetatable(l, err)
    l.Push(lua.LNil)
    l.Push(err)
    return 2
}

// For internal errors without Go error
func internalErrorMsg(l *lua.LState, msg string) int {
    err := lua.NewLuaError(l, msg).
        WithKind(lua.KindInternal).
        WithRetryable(false)
    l.Push(lua.LNil)
    l.Push(err)
    return 2
}
```

### Error Kinds

- `lua.KindInvalid` - Invalid input, bad arguments
- `lua.KindInternal` - Internal failures, Go errors

### Usage

```go
func encodeFunc(l *lua.LState) int {
    str, ok := l.Get(1).(lua.LString)
    if !ok {
        err := lua.NewLuaError(l, "string expected").
            WithKind(lua.KindInvalid).
            WithRetryable(false)
        l.Push(lua.LNil)
        l.Push(err)
        return 2
    }

    result, goErr := doSomething(string(str))
    if goErr != nil {
        err := lua.WrapErrorWithLua(l, goErr, "encode failed").
            WithKind(lua.KindInternal).
            WithRetryable(false)
        lua.SetErrorMetatable(l, err)
        l.Push(lua.LNil)
        l.Push(err)
        return 2
    }

    l.Push(lua.LString(result))
    return 1
}
```

## Typed UserData (for objects)

For modules with object types (like regexp, differ):

```go
const (
    typeRegexp = "text.Regexp"
)

type Regexp struct {
    re *regexp.Regexp
}

func init() {
    value.RegisterTypeMethods(nil, typeRegexp, nil, map[string]lua.LGFunction{
        "match_string":     regexpMatchString,
        "find_string":      regexpFindString,
        "replace_all_string": regexpReplaceAllString,
    })
}

func luaRegexpCompile(l *lua.LState) int {
    pattern := l.CheckString(1)

    re, err := regexp.Compile(pattern)
    if err != nil {
        luaErr := lua.WrapErrorWithLua(l, err, "regex compile error").
            WithKind(lua.KindInvalid).
            WithRetryable(false)
        lua.SetErrorMetatable(l, luaErr)
        l.Push(lua.LNil)
        l.Push(luaErr)
        return 2
    }

    value.PushTypedUserData(l, &Regexp{re: re}, typeRegexp)
    l.Push(lua.LNil)
    return 2
}

func regexpMatchString(l *lua.LState) int {
    ud := l.CheckUserData(1)
    wrapper, ok := ud.Value.(*Regexp)
    if !ok {
        l.ArgError(1, "expected text.Regexp")
        return 0
    }
    content := l.CheckString(2)
    l.Push(lua.LBool(wrapper.re.MatchString(content)))
    return 1
}
```

## Go Tests

### Basic Test Structure

```go
package mymodule

import (
    "testing"
    lua "github.com/yuin/gopher-lua"
)

func TestLoad(t *testing.T) {
    l := lua.NewState()
    defer l.Close()

    Module.Load(l)

    mod := l.GetGlobal("mymodule")
    if mod.Type() != lua.LTTable {
        t.Fatal("module not registered")
    }

    tbl := mod.(*lua.LTable)
    if tbl.RawGetString("encode").Type() != lua.LTFunction {
        t.Error("encode function not registered")
    }
}

func TestLoadReuse(t *testing.T) {
    l1 := lua.NewState()
    defer l1.Close()
    l2 := lua.NewState()
    defer l2.Close()

    Module.Load(l1)
    Module.Load(l2)

    mod1 := l1.GetGlobal("mymodule").(*lua.LTable)
    mod2 := l2.GetGlobal("mymodule").(*lua.LTable)

    if mod1 != mod2 {
        t.Error("module table should be reused across states")
    }
}
```

### Error Kind Tests

```go
func TestEncodeInvalidInput(t *testing.T) {
    l := lua.NewState()
    defer l.Close()
    lua.OpenErrors(l)  // Required for errors.INVALID constant
    Module.Load(l)

    err := l.DoString(`
        local result, err = mymodule.encode(123)
        if result ~= nil then
            error("expected nil result")
        end
        if err == nil then
            error("expected error")
        end
        if err:kind() ~= errors.INVALID then
            error("expected Invalid kind, got: " .. tostring(err:kind()))
        end
        if err:retryable() ~= false then
            error("expected retryable to be false")
        end
    `)
    if err != nil {
        t.Errorf("test failed: %v", err)
    }
}
```

## App Tests (Lua)

### _index.yaml

```yaml
version: "1.0"
namespace: app.test.mymodule

entries:
  - name: encode
    kind: function.lua
    meta:
      type: test
      suite: mymodule
      description: mymodule.encode function
    source: file://encode.lua
    method: main
    modules:
      - mymodule
    imports:
      assert_primitives: app.lib:assert

  - name: errors
    kind: function.lua
    meta:
      type: test
      suite: mymodule
      description: mymodule error handling
    source: file://errors.lua
    method: main
    modules:
      - mymodule
    imports:
      assert_primitives: app.lib:assert
```

### Test Files

```lua
-- encode.lua
local assert = require("assert_primitives")

local function main()
    local mymodule = require("mymodule")

    -- Success case
    local result, err = mymodule.encode({name = "test"})
    assert.is_nil(err, "encode should not error")
    assert.not_nil(result, "result returned")

    return true
end

return { main = main }
```

```lua
-- errors.lua
local assert = require("assert_primitives")

local function main()
    local mymodule = require("mymodule")

    -- Invalid input type
    local result, err = mymodule.encode(123)
    assert.is_nil(result, "invalid input returns nil")
    assert.not_nil(err, "error returned")
    assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
    assert.eq(err:retryable(), false, "not retryable")

    -- Internal error (if applicable)
    local r2, err2 = mymodule.decode("invalid-data")
    assert.is_nil(r2, "invalid data returns nil")
    assert.not_nil(err2, "error returned")
    assert.eq(err2:kind(), errors.INTERNAL, "error kind is INTERNAL")

    return true
end

return { main = main }
```

### Assert Functions

Available from `assert_primitives` (via `app.lib:assert`):

- `assert.eq(a, b, msg)` - Equal
- `assert.not_nil(v, msg)` - Not nil
- `assert.is_nil(v, msg)` - Is nil
- `assert.ok(v, msg)` - Truthy
- `assert.contains(s, substr, msg)` - String contains

### Error Constants

Always use `errors.*` constants (global):

- `errors.INVALID` - Invalid input
- `errors.INTERNAL` - Internal error

Never use string comparison like `"Invalid"`.

## spec.md Template

```markdown
# Lua MyModule Module Specification

## Overview

Brief description.

## Module Interface

### Module Loading

\`\`\`lua
local mymodule = require("mymodule")
\`\`\`

### Functions

#### mymodule.encode(data: table)

Description.

Parameters:
- `data`: Table to encode.

Returns:
- `encoded`: Result (or nil on error).
- `error`: Structured error object (or nil on success).

## Error Handling

### Error Types

1. **Invalid Input Type:**

\`\`\`lua
local result, err = mymodule.encode(123)
-- err:kind() == errors.INVALID
-- err:retryable() == false
\`\`\`

### Error Kind Comparison

Always use `errors.*` constants:

\`\`\`lua
if err:kind() == errors.INVALID then
    -- handle
end
\`\`\`

## Example Usage

\`\`\`lua
local mymodule = require("mymodule")
local result = mymodule.encode({name = "test"})
\`\`\`

## Thread Safety

Module is thread-safe. Tables are immutable.

## Module Classification

- **Class**: `encoding`, `deterministic`

## Go Implementation

\`\`\`go
var Module = &luaapi.ModuleDef{
    Name:        "mymodule",
    Description: "...",
    Class:       []string{...},
    Build:       buildModule,
}
\`\`\`
```

## Checklist

When creating/updating a module:

1. [ ] Use `ModuleDef` format
2. [ ] Use `lua.CreateTable(0, N)` with exact size
3. [ ] Set `mod.Immutable = true`
4. [ ] Return structured errors with `lua.NewLuaError` or `lua.WrapErrorWithLua`
5. [ ] Set error kind (`lua.KindInvalid` or `lua.KindInternal`)
6. [ ] Set `WithRetryable(false)` for non-retryable errors
7. [ ] Call `lua.SetErrorMetatable(l, err)` for wrapped errors
8. [ ] Go tests:
   - [ ] TestLoad - module registers correctly
   - [ ] TestLoadReuse - table reused across states
   - [ ] Error kind/retryable checks in error tests
   - [ ] Call `lua.OpenErrors(l)` before testing error kinds
9. [ ] App tests:
   - [ ] _index.yaml with test entries
   - [ ] Import `assert_primitives: app.lib:assert`
   - [ ] Use `errors.INVALID` / `errors.INTERNAL` constants
   - [ ] errors.lua for error handling tests
10. [ ] spec.md documentation
11. WE DO NOT SUPPORT OLD BIND() FORMAT, REMOVE IT.

## Running Tests

```bash
# Go tests
go test ./runtime/lua/modules/mymodule/... -v

# App tests
cd app && ./test.sh
```