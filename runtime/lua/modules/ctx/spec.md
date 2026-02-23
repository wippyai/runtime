<!-- SPDX-License-Identifier: MPL-2.0 -->

# ctx

Read-only context value access. Nondeterministic.

## Loading

```lua
local ctx = require("ctx")
```

## Functions

### get(key: string) → any, error

Retrieves a single value from the execution context by key.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Context key to retrieve |

**Returns:** Value of any type (string, number, boolean, table) and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key is empty string | errors.INVALID | no |
| key not found in context | errors.NOT_FOUND | no |
| no context available | errors.INTERNAL | no |

**Notes:**
- Values can be any Lua type: string, number, boolean, table (map or array)
- Tables are recursively converted from Go types
- Context values are read-only; use this to access values passed from parent processes or functions

### all() → table, error

Retrieves all context values as a table.

**Returns:** Table with all context key-value pairs, or empty table if no values exist. Always returns `table, nil` (no errors).

**Notes:**
- Returns empty table `{}` when no context values are set
- Table keys are strings, values can be any type
- Useful for inspecting all available context data

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local val, err = ctx.get("request_id")
if err then
    if err:kind() == errors.NOT_FOUND then
        -- key doesn't exist in context
    elseif err:kind() == errors.INVALID then
        -- empty key provided
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.INTERNAL`

## Example

```lua
local ctx = require("ctx")

-- Get single value
local request_id, err = ctx.get("request_id")
if err then
    if err:kind() == errors.NOT_FOUND then
        print("No request_id in context")
    else
        error(err)
    end
end

-- Get complex value (table)
local config, err = ctx.get("config")
if not err then
    print(config.max_retries, config.timeout)
end

-- Get all context values
local all, err = ctx.all()
for key, value in pairs(all) do
    print(key, value)
end
```
