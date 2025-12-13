# env

Environment variable access. Process, nondeterministic.

## Loading

```lua
local env = require("env")
```

## Functions

### get(key: string) → string, error

Gets the value of an environment variable.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Environment variable name |

**Returns:** `string` - Variable value, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Empty key | errors.INVALID | no |
| Variable not found | errors.NOT_FOUND | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| No context | errors.INTERNAL | no |
| Registry not found | errors.INTERNAL | no |

**Notes:**
- Subject to security policy restrictions
- May access OS environment or runtime-managed storage

### set(key: string, value: string) → boolean, error

Sets the value of an environment variable.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Environment variable name |
| value | string | yes | - | Value to set |

**Returns:** `true` on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Empty key | errors.INVALID | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| No context | errors.INTERNAL | no |
| Registry not found | errors.INTERNAL | no |

**Notes:**
- Subject to security policy restrictions
- May write to runtime-managed storage, not OS environment
- Overwrites existing values

### get_all() → table, error

Gets all accessible environment variables.

**Returns:** `table` - Map of variable names to values, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context | errors.INTERNAL | no |
| Registry not found | errors.INTERNAL | no |

**Notes:**
- Only includes variables permitted by security policy
- Returns both OS environment and runtime-managed variables
- Empty table if no variables are accessible

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local val, err = env.get("MY_VAR")
if err then
    if err:kind() == errors.NOT_FOUND then
        -- variable doesn't exist
    elseif err:kind() == errors.PERMISSION_DENIED then
        -- access denied by security policy
    elseif err:kind() == errors.INVALID then
        -- empty key
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.PERMISSION_DENIED`, `errors.INTERNAL`

## Example

```lua
local env = require("env")

-- Get environment variable
local path, err = env.get("PATH")
if err then error(err) end
print(path)

-- Set environment variable
local ok, err = env.set("MY_VAR", "my_value")
if err then error(err) end

-- Read it back
local val, err = env.get("MY_VAR")
if err then error(err) end
print(val)  -- "my_value"

-- Get all accessible variables
local all, err = env.get_all()
if err then error(err) end
for k, v in pairs(all) do
    print(k, v)
end
```
