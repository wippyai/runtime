<!-- SPDX-License-Identifier: MPL-2.0 -->

# Errors Specification

Structured error handling for Lua modules using `github.com/wippyai/go-lua` errors.

## Error Kind Constants

Use `errors.*` constants instead of hardcoded strings for kind comparison:

| Constant | Value | Description |
|----------|-------|-------------|
| `errors.NOT_FOUND` | `"NotFound"` | Resource not found |
| `errors.ALREADY_EXISTS` | `"AlreadyExists"` | Resource already exists |
| `errors.INVALID` | `"Invalid"` | Invalid input or argument |
| `errors.PERMISSION_DENIED` | `"PermissionDenied"` | Access denied |
| `errors.UNAVAILABLE` | `"Unavailable"` | Service temporarily unavailable |
| `errors.INTERNAL` | `"Internal"` | Internal error |
| `errors.CANCELED` | `"Canceled"` | Operation canceled |
| `errors.CONFLICT` | `"Conflict"` | Resource conflict |
| `errors.TIMEOUT` | `"Timeout"` | Operation timed out |
| `errors.RATE_LIMITED` | `"RateLimited"` | Rate limit exceeded |
| `errors.UNKNOWN` | `""` | Unknown/unspecified error |

## Error Methods

All errors returned by modules have these methods:

| Method | Returns | Description |
|--------|---------|-------------|
| `err:kind()` | `string` | Error category (use constants for comparison) |
| `err:retryable()` | `boolean\|nil` | Explicit retry setting, or nil when unspecified |
| `err:message()` | `string` | Error message |
| `err:details()` | `table\|nil` | Additional structured data |
| `err:stack()` | `string` | Lua stack trace |
| `tostring(err)` | `string` | Full error string representation |

## Creating Errors in Lua

```lua
local err = errors.new({
    message = "something failed",
    kind = errors.INVALID,
    retryable = false,
    details = { field = "name", reason = "too short" },
})

-- Wrap existing error with context
local wrapped = errors.wrap(err, "validation failed")

-- Check if error matches kind
if errors.is(err, errors.INVALID) then
    -- handle invalid input
end
```

`message` is required when using the table constructor. `retryable` may be omitted; then `err:retryable()` returns nil. A string constructor, `errors.new("message")`, has no explicit kind or retry setting.

Returning `nil, err` and raising `error(err)` preserve a native error's outer kind, message, retry setting, and details across function and process calls. Raising ends the current execution. A caller can return or raise the received error again. `pcall` catches the original native value locally. Plain strings, forged tables, and VM faults are execution errors with Internal kind and retryable false at a runtime boundary.

## Creating Errors in Go Modules

```go
// Simple error
err := lua.NewLuaError(l, "string expected").
    WithKind(lua.Invalid).
    WithRetryable(false)
l.Push(lua.LNil)
l.Push(err)
return 2

// Wrap Go error
decoded, goErr := base64.StdEncoding.DecodeString(input)
if goErr != nil {
    err := lua.WrapErrorWithLua(l, goErr, "decode failed").
        WithKind(lua.Invalid).
        WithRetryable(false)
    l.Push(lua.LNil)
    l.Push(err)
    return 2
}
```

## Checking Error Kind (Correct)

```lua
local result, err = some_operation()
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid input
    elseif err:kind() == errors.NOT_FOUND then
        -- handle not found
    elseif err:kind() == errors.TIMEOUT then
        -- handle timeout, maybe retry
    end
end
```

## Checking Error Kind (Wrong)

```lua
-- DO NOT hardcode strings
if err:kind() == "Invalid" then  -- wrong
    ...
end

-- Use constants instead
if err:kind() == errors.INVALID then  -- correct
    ...
end
```

## Return Convention

Modules return errors as second value:

```lua
local result, err = module.operation(args)
if err then
    -- handle error
    return nil, err
end
-- use result
```

## Retryable Errors

Check `retryable()` before retry logic:

```lua
local result, err = http.get(url)
if err and err:retryable() then
    -- safe to retry
    time.sleep(1000)
    result, err = http.get(url)
end
```

Kind and retryability are separate metadata. No retry decision is inferred from message text or kind. An omitted retry setting stays nil across runtime calls.

## Error Details

Access structured error data:

```lua
local _, err = validate(data)
if err then
    local details = err:details()
    if details then
        print("Field:", details.field)
        print("Reason:", details.reason)
    end
end
```

## Go Module Implementation

Required imports:
```go
import lua "github.com/wippyai/go-lua"
```

Available Go constants:
- `lua.Unknown`
- `lua.NotFound`
- `lua.AlreadyExists`
- `lua.Invalid`
- `lua.PermissionDenied`
- `lua.Unavailable`
- `lua.Internal`
- `lua.Canceled`
- `lua.Conflict`
- `lua.Timeout`
- `lua.RateLimited`

Error creation functions:
- `lua.NewLuaError(l, message)` - Create new error with Lua stack (metatable set automatically)
- `lua.WrapErrorWithLua(l, err, context)` - Wrap Go error (metatable set automatically)
