<!-- SPDX-License-Identifier: MPL-2.0 -->

# store

Key-value store operations with resource management. Storage, io, nondeterministic.

## Loading

```lua
local store = require("store")
```

## Functions

### get(id: string) → Store, error

Acquires a store resource by ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Resource ID (e.g., "app.test.store:memory") |

**Returns:**
- Success: `Store` - Store object with methods
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`, `:retryable()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| id is empty string | errors.INVALID | no |
| registry not found | errors.NOT_FOUND | no |
| resource not found | errors.INTERNAL | no |
| resource not a store | errors.INVALID | no |

**Yields:** until resource acquired

## Types

### Store

Returned by `store.get()`. Manages key-value operations and resource lifecycle.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| get | (key: string) | value: any, error | Retrieves value by key |
| set | (key: string, value: any, ttl?: number) | boolean, error | Stores value with optional TTL in seconds |
| has | (key: string) | boolean, error | Checks if key exists |
| delete | (key: string) | boolean, error | Deletes key, returns false if not found |
| release | () | boolean | Releases resource, idempotent |

#### store:get(key: string) → any, error

Retrieves value for key.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Key to retrieve |

**Returns:**
- Success: `value, nil` - Stored value (string, number, boolean, table, etc.)
- Error: `nil, error` - error is string

**Errors (strings):**
- Store released
- Key is empty
- Key not found
- Security policy violation

**Yields:** until value retrieved

#### store:set(key: string, value: any, ttl?: number) → boolean, error

Stores value at key with optional TTL.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Key to store |
| value | any | yes | - | Value to store (cannot be nil) |
| ttl | number | no | 0 | Time-to-live in seconds (0 = no expiration) |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is string

**Errors (strings):**
- Store released
- Key is empty
- Value is nil
- Security policy violation

**Yields:** until value stored

#### store:has(key: string) → boolean, error

Checks if key exists.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Key to check |

**Returns:**
- Success: `boolean, nil` - true if exists, false if not
- Error: `false, error` - error is string

**Errors (strings):**
- Store released
- Key is empty
- Security policy violation

**Yields:** until existence checked

#### store:delete(key: string) → boolean, error

Deletes key from store.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Key to delete |

**Returns:**
- Success: `true, nil` - key was deleted
- Success: `false, nil` - key didn't exist
- Error: `nil, error` - error is string

**Errors (strings):**
- Store released
- Key is empty
- Security policy violation

**Yields:** until deletion complete

#### store:release() → boolean

Releases the store resource. Safe to call multiple times.

**Returns:** `true` - always returns true

## Errors

This module uses mixed error types:

`store.get()` returns structured errors. Check kind with `errors.*` constants:

```lua
local s, err = store.get("test:store")
if err then
    if err:kind() == errors.INVALID then
        -- invalid parameters
    elseif err:kind() == errors.NOT_FOUND then
        -- resource not found
    elseif err:kind() == errors.INTERNAL then
        -- internal error
    end
end
```

Store methods (`get`, `set`, `has`, `delete`) return string errors:

```lua
local val, err = s:get("mykey")
if err then
    print("Error: " .. err)  -- error is a string
end
```

## Example

```lua
local store = require("store")

local s, err = store.get("app.test.store:memory")
if err then error(err) end

-- Set various types of values
s:set("user:name", "alice")
s:set("user:age", 25)
s:set("user:active", true)
s:set("user:profile", {name = "Alice", role = "admin"})

-- Set with TTL (300 seconds)
s:set("session:token", "abc123", 300)

-- Get values
local name, err = s:get("user:name")
if err then
    print("Error: " .. err)
else
    print("Name: " .. name)
end

-- Check existence
local exists, err = s:has("user:name")
if exists then
    print("Key exists")
end

-- Delete key
local ok, err = s:delete("user:name")
if ok then
    print("Key deleted")
else
    print("Key didn't exist")
end

-- Release when done
s:release()
```
