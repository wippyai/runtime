# Lua UUID Module Specification

## Overview

The `uuid` module provides UUID generation and validation. This module must be loaded via `require("uuid")` or declared in the `modules:` section of the function definition.

## Module Interface

```lua
local uuid = require("uuid")
local id = uuid.v4()
```

## Functions

### uuid.v1()

Generates a version 1 (time-based) UUID.

Returns:

- `uuid`: UUID string (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local id, err = uuid.v1()
-- id: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
```

### uuid.v3(namespace, name)

Generates a version 3 (MD5 hash-based) UUID.

Parameters:

- `namespace`: UUID string to use as namespace.
- `name`: String to hash with the namespace.

Returns:

- `uuid`: UUID string (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
local id, err = uuid.v3(ns, "example.com")
```

### uuid.v4()

Generates a version 4 (random) UUID.

Returns:

- `uuid`: UUID string (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local id, err = uuid.v4()
-- id: "550e8400-e29b-41d4-a716-446655440000"
```

### uuid.v5(namespace, name)

Generates a version 5 (SHA-1 hash-based) UUID.

Parameters:

- `namespace`: UUID string to use as namespace.
- `name`: String to hash with the namespace.

Returns:

- `uuid`: UUID string (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
local id, err = uuid.v5(ns, "example.com")
```

### uuid.v7()

Generates a version 7 (Unix timestamp-based) UUID.

Returns:

- `uuid`: UUID string (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local id, err = uuid.v7()
```

### uuid.validate(input)

Checks if a string is a valid UUID.

Parameters:

- `input`: String to validate.

Returns:

- `valid`: Boolean indicating if the input is a valid UUID.
- `nil`: Always returns nil as second value.

```lua
local valid = uuid.validate("550e8400-e29b-41d4-a716-446655440000")
-- valid: true

local invalid = uuid.validate("not-a-uuid")
-- invalid: false
```

### uuid.version(uuid)

Returns the version number of a UUID.

Parameters:

- `uuid`: UUID string.

Returns:

- `version`: Integer version number (1, 3, 4, 5, 7, etc.) or nil on error.
- `error`: Structured error (or nil on success).

```lua
local ver, err = uuid.version("550e8400-e29b-41d4-a716-446655440000")
-- ver: 4
```

### uuid.variant(uuid)

Returns the variant of a UUID as a string.

Parameters:

- `uuid`: UUID string.

Returns:

- `variant`: String variant name or nil on error.
- `error`: Structured error (or nil on success).

Variant values:

- `"RFC4122"` - Standard RFC 4122 variant.
- `"Microsoft"` - Microsoft reserved variant.
- `"NCS"` - NCS backward compatibility variant.
- `"Invalid"` - Invalid variant.

```lua
local var, err = uuid.variant("550e8400-e29b-41d4-a716-446655440000")
-- var: "RFC4122"
```

### uuid.parse(uuid)

Parses a UUID and returns detailed information.

Parameters:

- `uuid`: UUID string.

Returns:

- `info`: Table with UUID information or nil on error.
- `error`: Structured error (or nil on success).

Info table fields:

- `version`: Integer version number.
- `variant`: String variant name.
- `timestamp`: Integer Unix timestamp (only for v1 and v7).
- `node`: String node ID (only for v1).

```lua
local info, err = uuid.parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
-- info.version: 1
-- info.variant: "RFC4122"
-- info.timestamp: 879006630
-- info.node: "00c04fd430c8"
```

### uuid.format(uuid, format)

Formats a UUID in different representations.

Parameters:

- `uuid`: UUID string.
- `format`: Format type (optional, defaults to "standard").

Format types:

- `"standard"` - Standard hyphenated format (default).
- `"simple"` - 32 hex characters without hyphens.
- `"urn"` - URN format with "urn:uuid:" prefix.

Returns:

- `formatted`: Formatted UUID string or nil on error.
- `error`: Structured error (or nil on success).

```lua
local id = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

local std = uuid.format(id, "standard")
-- std: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

local simple = uuid.format(id, "simple")
-- simple: "6ba7b8109dad11d180b400c04fd430c8"

local urn = uuid.format(id, "urn")
-- urn: "urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8"
```

## Error Handling

The module returns structured errors using the standard error type.

### Error Types

1. **Invalid Error:** Input validation failures.

```lua
local id, err = uuid.v3("invalid", "test")
if err then
    -- err:kind() == errors.INVALID
    -- err:retryable() == false
end
```

2. **Internal Error:** UUID generation failures.

```lua
local id, err = uuid.v1()
if err then
    -- err:kind() == errors.INTERNAL
    -- err:retryable() == false
end
```

## Determinism

- `uuid.v3` and `uuid.v5` are deterministic: same namespace and name always produce the same UUID.
- `uuid.v1`, `uuid.v4`, and `uuid.v7` are non-deterministic: each call produces a unique UUID.

## Module Classification

- **Class**: `nondeterministic`
- The module is marked as non-deterministic because most generation functions produce unique values on each call.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "uuid",
    Description: "UUID generation and validation",
    Class:       []string{luaapi.ClassNondeterministic},
    Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
    mod := lua.CreateTable(0, 10)
    mod.RawSetString("v1", lua.LGoFunc(uuidV1))
    mod.RawSetString("v3", lua.LGoFunc(uuidV3))
    mod.RawSetString("v4", lua.LGoFunc(uuidV4))
    mod.RawSetString("v5", lua.LGoFunc(uuidV5))
    mod.RawSetString("v7", lua.LGoFunc(uuidV7))
    mod.RawSetString("validate", lua.LGoFunc(uuidValidate))
    mod.RawSetString("version", lua.LGoFunc(uuidVersion))
    mod.RawSetString("variant", lua.LGoFunc(uuidVariant))
    mod.RawSetString("parse", lua.LGoFunc(uuidParse))
    mod.RawSetString("format", lua.LGoFunc(uuidFormat))
    mod.Immutable = true
    return mod, nil
}
```
