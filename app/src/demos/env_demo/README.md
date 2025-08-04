# Environment Storage Demo

This demo showcases different types of environment variable storage backends available in the runtime system.

## Storage Types

### 1. Memory Storage (`env.storage.memory`)
- **Type**: Read-write
- **Persistence**: In-memory only (lost on restart)
- **Use case**: Temporary variables, caching, testing

### 2. File Storage (`env.storage.file`)
- **Type**: Read-write
- **Persistence**: File-based (survives restarts)
- **Use case**: Configuration files, persistent settings

### 3. OS Storage (`env.storage.os`) - NEW!
- **Type**: Read-only
- **Persistence**: System environment variables
- **Use case**: Accessing system environment variables

## OS Storage Feature

The new `env.storage.os` feature allows you to access operating system environment variables in a read-only manner. This is useful for:

- Accessing system configuration (PATH, HOME, USER, etc.)
- Reading environment variables set by the system or container
- Integrating with external tools that rely on environment variables

### Configuration

The OS storage is configured in `system.yaml`:

```yaml
- name: envos
  kind: env.storage.os
  meta:
    type: envstorage
    comment: OS storage for environment variables (read-only)
```

### Usage Examples

```yaml
# Access system PATH variable
- name: path_env
  kind: env.variable
  meta:
    type: secret_key
    depends_on:
      - system:envos
  variable: PATH
  storage: system:envos
  default:
  readonly: true

# Access user home directory
- name: home_env
  kind: env.variable
  meta:
    type: secret_key
    depends_on:
      - system:envos
  variable: HOME
  storage: system:envos
  default:
  readonly: true
```

### Lua API Usage

```lua
local env = require("env")

-- Get system environment variables
local path = env.get('path_env')        -- Gets PATH
local home = env.get('home_env')        -- Gets HOME
local user = env.get('user_env')        -- Gets USER

-- Try to set (will fail - read-only)
local success = env.set('path_env', 'new_path')  -- Returns false
```

## Testing the Demo

1. Start the runtime system
2. Navigate to `/env/demo` endpoint
3. View the test results showing:
   - Memory storage operations
   - File storage operations
   - OS storage operations (new!)

## Key Features of OS Storage

- **Read-only**: Cannot modify system environment variables
- **System integration**: Accesses actual OS environment variables
- **Cross-platform**: Works on Linux, Windows, macOS
- **Container-friendly**: Works in Docker containers
- **Security**: Respects system permissions and security policies

## Test Cases

The demo includes tests for:

1. **Basic OS variable access**: Reading system environment variables
2. **Full name access**: Using namespace-qualified variable names
3. **ENV_NAME access**: Using environment variable names directly
4. **Read-only enforcement**: Attempting to set OS variables (should fail)
5. **Common system variables**: PATH, HOME, USER
6. **Non-existent variables**: Handling missing environment variables

## Benefits

- **System integration**: Access real system environment variables
- **Security**: Read-only access prevents accidental modifications
- **Compatibility**: Works with existing system tools and scripts
- **Containerization**: Supports Docker and other container environments
- **Cross-platform**: Works across different operating systems 