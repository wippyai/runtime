# Errors Specification

Structured error handling for Lua modules using gopher-lua's `lua.Error` type.

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
| `err:retryable()` | `boolean` | Whether operation can be retried |
| `err:message()` | `string` | Error message |
| `err:details()` | `table\|nil` | Additional structured data |
| `err:stack()` | `string` | Lua stack trace |
| `tostring(err)` | `string` | Full error string representation |

## Creating Errors in Lua

```lua
-- Create new error
local err = errors.new("something failed")
    :kind(errors.INVALID)
    :retryable(false)
    :details({ field = "name", reason = "too short" })

-- Wrap existing error with context
local wrapped = errors.wrap(err, "validation failed")

-- Check if error matches kind
if errors.is(err, errors.INVALID) then
    -- handle invalid input
end
```

## Creating Errors in Go Modules

```go
// Simple error
err := lua.NewLuaError(l, "string expected").
    WithKind(lua.KindInvalid).
    WithRetryable(false)
l.Push(lua.LNil)
l.Push(err)
return 2

// Wrap Go error
decoded, goErr := base64.StdEncoding.DecodeString(input)
if goErr != nil {
    err := lua.WrapErrorWithLua(l, goErr, "decode failed").
        WithKind(lua.KindInvalid).
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

Typically retryable:
- `errors.TIMEOUT`
- `errors.UNAVAILABLE`
- `errors.RATE_LIMITED`

Typically not retryable:
- `errors.INVALID`
- `errors.NOT_FOUND`
- `errors.PERMISSION_DENIED`
- `errors.ALREADY_EXISTS`

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
import lua "github.com/yuin/gopher-lua"
```

Available Go constants:
- `lua.KindUnknown`
- `lua.KindNotFound`
- `lua.KindAlreadyExists`
- `lua.KindInvalid`
- `lua.KindPermissionDenied`
- `lua.KindUnavailable`
- `lua.KindInternal`
- `lua.KindCanceled`
- `lua.KindConflict`
- `lua.KindTimeout`
- `lua.KindRateLimited`

Error creation functions:
- `lua.NewLuaError(l, message)` - Create new error with Lua stack (metatable set automatically)
- `lua.WrapErrorWithLua(l, err, context)` - Wrap Go error (metatable set automatically)
