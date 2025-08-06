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

### 3. OS Storage (`env.storage.os`)
- **Type**: Read-only
- **Persistence**: System environment variables
- **Use case**: Accessing system environment variables

### 4. Router Storage (`env.storage.router`) - NEW!
- **Type**: Composite storage with fallback
- **Persistence**: Depends on underlying storages
- **Use case**: Combining multiple storages with fallback mechanism

## Router Storage Feature

The new `env.storage.router` feature provides a composite storage that combines multiple storage backends with a fallback mechanism:

- **Reads**: Attempts to get values from storages in order until a value is found
- **Writes**: Always writes to the primary (first) storage only
- **Fallback**: If a variable is not found in the primary storage, it tries the next storage in the chain

### Configuration

The router storage is configured in `system.yaml`:

```yaml
- name: app_envs
  kind: env.storage.router
  meta:
    type: envstorage
    comment: Composite storage with fallback mechanism
  storages:
    - system:envmemory
    - system:envos
```

This configuration creates a router that:
1. First tries to read from `envmemory` (memory storage)
2. If not found, falls back to `envos` (OS environment variables)
3. Writes always go to `envmemory` (the primary storage)

### Router Storage Usage Examples

#### Basic Variable Access

```yaml
# Define variables that use the router storage
- name: app_path
  kind: env.variable
  meta:
    type: secret_key
    depends_on:
      - system:app_envs
  variable: APP_PATH
  storage: system:app_envs
  default: /opt/app

- name: system_path
  kind: env.variable
  meta:
    type: secret_key
    depends_on:
      - system:app_envs
  variable: PATH
  storage: system:app_envs
  default:
  readonly: true
```

#### Lua API Usage

```lua
local env = require("env")

-- Reading with fallback mechanism
local app_path = env.get('app_path')           -- Gets from memory if set, otherwise default
local system_path = env.get('system_path')     -- Gets from memory if set, otherwise from OS
local home_dir = env.get('system:app_envs:HOME') -- Gets from memory if set, otherwise from OS

-- Writing (always goes to primary storage - memory)
env.set('app_path', '/custom/app/path')        -- Stores in memory
env.set('system:app_envs:CUSTOM_VAR', 'value') -- Stores in memory

-- Reading non-existent variables
local missing = env.get('system:app_envs:NONEXISTENT') -- Returns error from last storage
```

#### Advanced Configuration Examples

```yaml
# Complex router with multiple storages
- name: production_envs
  kind: env.storage.router
  meta:
    type: envstorage
    comment: Production environment with multiple fallbacks
  storages:
    - system:envmemory      # Primary: Fast in-memory storage
    - system:envfile        # Secondary: File-based configuration
    - system:envos          # Tertiary: System environment variables

# Development router with different priorities
- name: dev_envs
  kind: env.storage.router
  meta:
    type: envstorage
    comment: Development environment configuration
  storages:
    - system:envfile        # Primary: File-based config (persistent)
    - system:envmemory      # Secondary: Memory for temporary overrides
    - system:envos          # Tertiary: System defaults
```

### OS Storage Feature

The `env.storage.os` feature allows you to access operating system environment variables in a read-only manner. This is useful for:

- Accessing system configuration (PATH, HOME, USER, etc.)
- Reading environment variables set by the system or container
- Integrating with external tools that rely on environment variables

#### Configuration

The OS storage is configured in `system.yaml`:

```yaml
- name: envos
  kind: env.storage.os
  meta:
    type: envstorage
    comment: OS storage for environment variables (read-only)
```

#### Usage Examples

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

#### Lua API Usage

```lua
local env = require("env")

-- Get system environment variables
local path = env.get('path_env')        -- Gets PATH
local home = env.get('home_env')        -- Gets HOME
local user = env.get('user_env')        -- Gets USER

-- Try to set (will fail - read-only)
local success = env.set('path_env', 'new_path')  -- Returns false
```

## Practical Use Cases

### 1. Application Configuration with Fallbacks

```lua
-- Application can set its own config, but fall back to system defaults
local config = {
    data_dir = env.get('system:app_envs:DATA_DIR') or '/var/data',
    log_level = env.get('system:app_envs:LOG_LEVEL') or 'INFO',
    api_key = env.get('system:app_envs:API_KEY') or env.get('system:app_envs:DEFAULT_API_KEY')
}
```

### 2. Development vs Production Environments

```yaml
# Development: File config takes precedence
- name: dev_config
  kind: env.storage.router
  storages:
    - system:envfile    # Local .env file
    - system:envos      # System defaults

# Production: Memory for runtime overrides
- name: prod_config
  kind: env.storage.router
  storages:
    - system:envmemory  # Runtime overrides
    - system:envfile    # Base configuration
    - system:envos      # System defaults
```

### 3. Container Environment Integration

```lua
-- In a Docker container, access both container env vars and system defaults
local container_id = env.get('system:app_envs:HOSTNAME')     -- Container hostname
local app_port = env.get('system:app_envs:APP_PORT')        -- App port from env
local system_path = env.get('system:app_envs:PATH')         -- System PATH
```

## Testing the Demo

1. Start the runtime system
2. Navigate to `/env/demo` endpoint
3. View the test results showing:
   - Memory storage operations
   - File storage operations
   - OS storage operations
   - Router storage operations (new!)

## Key Features

### Router Storage Benefits
- **Flexible Configuration**: Combine different storage types
- **Fallback Mechanism**: Automatic fallback to system environment variables
- **Write Control**: All writes go to the primary storage for consistency
- **Performance**: Memory storage provides fast access for frequently used variables
- **Persistence**: OS storage provides access to system environment variables

### OS Storage Benefits
- **System integration**: Access real system environment variables
- **Security**: Read-only access prevents accidental modifications
- **Compatibility**: Works with existing system tools and scripts
- **Containerization**: Supports Docker and other container environments
- **Cross-platform**: Works across different operating systems

## Test Cases

The demo includes tests for:

1. **Basic OS variable access**: Reading system environment variables
2. **Full name access**: Using namespace-qualified variable names
3. **ENV_NAME access**: Using environment variable names directly
4. **Read-only enforcement**: Attempting to set OS variables (should fail)
5. **Common system variables**: PATH, HOME, USER
6. **Non-existent variables**: Handling missing environment variables
7. **Router fallback mechanism**: Testing fallback between storages
8. **Router write behavior**: Ensuring writes go to primary storage
9. **Router list operation**: Combining variables from all storages 