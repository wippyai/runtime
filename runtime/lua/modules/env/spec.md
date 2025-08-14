# Lua Environment Module Specification

## Overview

The `env` module provides a Lua interface for accessing environment variables that are shared from the Go runtime. Environment variables can be accessed by ID (`ns:name` format) or by variable short name, with fallback mechanisms through router storage.

## Module Interface

### Module Loading

```lua
local env = require("env")
```

### Functions

#### env.get(key: string)

Retrieves the value of a specific environment variable.

Parameters:
- `key`: String identifier for the environment variable. Can be:
    - Variable ID: `app.env:database_url` (registry entry ID)
    - Environment variable name: `DATABASE_URL` (actual env var name)

Returns:
- `value`: The value of the environment variable (or nil if not found)
- `error`: Error message string (or nil on success)

Example:

```lua
-- Using variable ID (registry entry)
local value, err = env.get("app.env:database_url")

-- Using environment variable name directly
local value, err = env.get("DATABASE_URL")
```

#### env.set(key: string, value: string)

Sets the value of a specific environment variable. Only works with writable variables.

Parameters:
- `key`: String identifier for the environment variable. Can be:
    - Variable ID: `app.env:database_url` (registry entry ID)
    - Environment variable name: `DATABASE_URL` (actual env var name)
- `value`: String value to set

Returns:
- `success`: Boolean true on success (or nil on error)
- `error`: Error message string (or nil on success)

Example:

```lua
-- Using variable ID
local success, err = env.set("app.env:database_url", "postgres://...")

-- Using environment variable name
local success, err = env.set("DATABASE_URL", "postgres://...")
```

Note: Read-only variables cannot be modified and will return an error.

Example:

```lua
local success, err = env.set("MY_VAR", "my_value")
if err then
    print("Error:", err)
else
    print("Variable set successfully")
end
```

#### env.get_all()

Retrieves all available environment variables as a table.

Returns:
- `table`: Table containing all environment variables (key-value pairs)
- `error`: Error message string (or nil on success)

Example:

```lua
local vars, err = env.get_all()
if err then
    print("Error:", err)
else
    for k, v in pairs(vars) do
        print(k, "=", v)
    end
end
```

## Security

All environment variable operations are subject to security policy checks:

- **Access Control**: Each `env.get()` and `env.set()` operation is validated against security policies
- **Operation-Specific Permissions**: Separate policies for read (`env.get`) and write (`env.set`) operations
- **Variable-Specific Permissions**: Policies can be applied per variable name or pattern
- **Filtered Results**: `env.get_all()` only returns variables the user has permission to access

Security violations will result in "Permission Denied" errors.

## Storage Types

- **File Storage**: Variables stored in `.env` files (usually writable)
- **OS Storage**: System environment variables (read-only)
- **Memory Storage**: Runtime variables (writable)
- **Router Storage**: Fallback mechanism across multiple storages

## Error Handling

The module returns errors in the following cases:

1. **Missing Context:** When no context is found or the context is invalid
2. **Empty Key:** When an empty key is provided
3. **Empty Value:** When an empty value is provided to `set`
4. **Variable Not Found:** When the requested environment variable doesn't exist
5. **Permission Denied:** When access to the variable is not allowed
6. **Read-Only Variable:** When attempting to modify a read-only variable

## Thread Safety

- The environment module is thread-safe
- Environment variables are managed by the Go runtime