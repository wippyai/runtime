# uuid

UUID generation and validation. Nondeterministic.

## Loading

```lua
local uuid = require("uuid")
```

## Functions

### v1() → string, error

Generates a version 1 (time-based) UUID.

**Returns:**
- Success: `string` - UUID in standard format (36 characters with hyphens)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| generation fails | errors.INTERNAL | no |

```lua
local id, err = uuid.v1()
-- id: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
```

### v3(namespace: string, name: string) → string, error

Generates a version 3 (MD5 hash-based) UUID. Deterministic.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| namespace | string | yes | - | Valid UUID string |
| name | string | yes | - | Value to hash with namespace |

**Returns:**
- Success: `string` - UUID in standard format
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| missing arguments | errors.INVALID | no |
| namespace not string | errors.INVALID | no |
| name not string | errors.INVALID | no |
| invalid namespace UUID | errors.INVALID | no |

```lua
local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
local id, err = uuid.v3(ns, "example.com")
-- Same namespace + name always produces same UUID
```

### v4() → string, error

Generates a version 4 (random) UUID.

**Returns:**
- Success: `string` - UUID in standard format
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| generation fails | errors.INTERNAL | no |

```lua
local id, err = uuid.v4()
-- id: "550e8400-e29b-41d4-a716-446655440000"
```

### v5(namespace: string, name: string) → string, error

Generates a version 5 (SHA-1 hash-based) UUID. Deterministic.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| namespace | string | yes | - | Valid UUID string |
| name | string | yes | - | Value to hash with namespace |

**Returns:**
- Success: `string` - UUID in standard format
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| missing arguments | errors.INVALID | no |
| namespace not string | errors.INVALID | no |
| name not string | errors.INVALID | no |
| invalid namespace UUID | errors.INVALID | no |

```lua
local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
local id, err = uuid.v5(ns, "example.com")
-- Same namespace + name always produces same UUID
```

### v7() → string, error

Generates a version 7 (Unix timestamp-based) UUID.

**Returns:**
- Success: `string` - UUID in standard format
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| generation fails | errors.INTERNAL | no |

```lua
local id, err = uuid.v7()
```

### validate(input: any) → boolean

Checks if input is a valid UUID string.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| input | any | yes | - | Value to validate |

**Returns:** `boolean` - true if valid UUID, false otherwise (never returns error)

```lua
local valid = uuid.validate("550e8400-e29b-41d4-a716-446655440000")
-- valid: true

local invalid = uuid.validate("not-a-uuid")
-- invalid: false

local notstring = uuid.validate(123)
-- notstring: false
```

### version(uuid: string) → integer, error

Extracts the version number from a UUID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| uuid | string | yes | - | Valid UUID string |

**Returns:**
- Success: `integer` - version number (1, 3, 4, 5, 7, etc.)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| input not string | errors.INVALID | no |
| invalid UUID format | errors.INVALID | no |

```lua
local ver, err = uuid.version("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
-- ver: 1
```

### variant(uuid: string) → string, error

Returns the variant of a UUID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| uuid | string | yes | - | Valid UUID string |

**Returns:**
- Success: `string` - variant name
- Error: `nil, error` - structured error

**Variant values:**
- `"RFC4122"` - Standard RFC 4122 variant
- `"Microsoft"` - Microsoft reserved variant
- `"NCS"` - NCS backward compatibility variant
- `"Invalid"` - Invalid variant

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| input not string | errors.INVALID | no |
| invalid UUID format | errors.INVALID | no |

```lua
local var, err = uuid.variant("550e8400-e29b-41d4-a716-446655440000")
-- var: "RFC4122"
```

### parse(uuid: string) → table, error

Parses a UUID and returns detailed information.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| uuid | string | yes | - | Valid UUID string |

**Returns:**
- Success: `table` - info table with UUID details
- Error: `nil, error` - structured error

**Info table fields:**

| Field | Type | Present When | Notes |
|-------|------|--------------|-------|
| version | integer | always | UUID version number |
| variant | string | always | Variant name (same as `variant()`) |
| timestamp | integer | v1, v7 only | Unix timestamp in seconds |
| node | string | v1 only | Node ID (MAC address) |

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| input not string | errors.INVALID | no |
| invalid UUID format | errors.INVALID | no |

```lua
local info, err = uuid.parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
-- info.version: 1
-- info.variant: "RFC4122"
-- info.timestamp: 879006630
-- info.node: "00c04fd430c8"

local info7, _ = uuid.parse(v7_uuid)
-- info7.version: 7
-- info7.timestamp: 1702483200
-- info7.node: nil (not present)
```

### format(uuid: string, format?: string) → string, error

Formats a UUID in different representations.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| uuid | string | yes | - | Valid UUID string |
| format | string | no | "standard" | Format type |

**Format types:**
- `"standard"` - Standard hyphenated format (default)
- `"simple"` - 32 hex characters without hyphens
- `"urn"` - URN format with "urn:uuid:" prefix

**Returns:**
- Success: `string` - formatted UUID
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| input not string | errors.INVALID | no |
| invalid UUID format | errors.INVALID | no |
| unsupported format type | errors.INVALID | no |

```lua
local id = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

local std, _ = uuid.format(id, "standard")
-- std: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

local simple, _ = uuid.format(id, "simple")
-- simple: "6ba7b8109dad11d180b400c04fd430c8"

local urn, _ = uuid.format(id, "urn")
-- urn: "urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8"

local default, _ = uuid.format(id)
-- default: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
```

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local id, err = uuid.v3("invalid-namespace", "test")
if err then
    if err:kind() == errors.INVALID then
        -- invalid input
    elseif err:kind() == errors.INTERNAL then
        -- generation failure
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

All errors are non-retryable (`err:retryable()` returns `false`).

## Example

```lua
local uuid = require("uuid")

-- Generate different UUID versions
local v4, err = uuid.v4()
if err then error(err) end

local v7, err = uuid.v7()
if err then error(err) end

-- Deterministic UUIDs
local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
local v5, err = uuid.v5(ns, "example.com")
if err then error(err) end

-- Validate and parse
if uuid.validate(v4) then
    local info, _ = uuid.parse(v4)
    print(info.version)  -- 4
    print(info.variant)  -- "RFC4122"
end

-- Format UUIDs
local simple, _ = uuid.format(v4, "simple")
print(simple)  -- "550e8400e29b41d4a716446655440000"
```
