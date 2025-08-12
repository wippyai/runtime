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

## Three Access Modes

The environment variable system supports **three different ways** to access variables:

### 1. By Name (e.g., "file_test_env")
- **Behavior**: Automatically adds current namespace to the variable name
- **Example**: `env.get('file_test_env')` → looks for `app.env.demo:file_test_env`
- **Use case**: Simple variable access within current context

### 2. By Full Name (e.g., "app.env.demo:file_test_env")
- **Behavior**: Explicit namespace specification
- **Example**: `env.get('app.env.demo:file_test_env')` → direct lookup
- **Use case**: Cross-namespace variable access

### 3. By ENV Name (e.g., "FILE_TEST_ENV")
- **Behavior**: Direct environment variable name lookup
- **Example**: `env.get('FILE_TEST_ENV')` → searches for variable with this ENV name
- **Use case**: Direct access to environment variables

### Cross-Verification
All three access modes return **identical values** for the same variable, ensuring consistency across different access patterns.

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
    - system:envfile
    - system:envos
```

This configuration creates a router that:
1. First tries to read from `envmemory` (memory storage)
2. If not found, falls back to `envfile` (file storage)
3. If not found, falls back to `envos` (OS environment variables)
4. Writes always go to `envmemory` (the primary storage)

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

## Comprehensive Testing

The environment variable system includes **comprehensive test coverage** for all storage types and access modes:

### Test Coverage Matrix

| Storage Type | By Name | By Full Name | By ENV Name | Read/Write | Default Values |
|--------------|---------|--------------|-------------|------------|----------------|
| **Memory**   | ✅      | ✅           | ✅          | ✅         | ✅             |
| **File**     | ✅      | ✅           | ✅          | ✅         | ✅             |
| **OS**       | ✅      | ✅           | ✅          | Read-only  | ✅             |
| **Router**   | ✅      | ✅           | ✅          | ✅         | ✅             |

### Test Categories

#### 1. **Three Access Modes Tests**
- **By Name**: `env.get('file_test_env')` → `app.env.demo:file_test_env`
- **By Full Name**: `env.get('app.env.demo:file_test_env')` → direct lookup
- **By ENV Name**: `env.get('FILE_TEST_ENV')` → environment variable lookup

#### 2. **Storage Type Tests**
- **Memory Storage**: In-memory operations with all access modes
- **File Storage**: File-based persistence with all access modes
- **OS Storage**: Read-only system environment variable access
- **Router Storage**: Fallback mechanism across multiple storages

#### 3. **Advanced Router Tests**
- **Complex Router**: Testing with all 3 underlying storage types
- **Fallback Mechanism**: Variables not in primary storage fall back to secondary
- **Write Behavior**: All writes go to primary storage only
- **Cross-Verification**: All access modes return identical values

#### 4. **Edge Case Tests**
- **Default Values**: Empty storage values fall back to defaults
- **Read-Only Enforcement**: OS storage prevents write operations
- **Missing Variables**: Proper error handling for non-existent variables
- **Namespace Isolation**: Variables in different namespaces don't interfere

### Test Examples

```lua
-- All three access modes return the same value
local value1 = env.get('file_test_env')           -- By name
local value2 = env.get('app.env.demo:file_test_env') -- By full name  
local value3 = env.get('FILE_TEST_ENV')           -- By ENV name
assert(value1 == value2 and value2 == value3)     -- All identical

-- Router storage fallback
local router_var = env.get('router_memory_var')   -- Gets from memory
local fallback_var = env.get('router_fallback_var') -- Gets from OS (fallback)

-- Cross-verification
env.set('router_memory_var', 'new_value')         -- Sets in primary storage
local v1 = env.get('router_memory_var')           -- By name
local v2 = env.get('app.env.demo:router_memory_var') -- By full name
local v3 = env.get('ROUTER_MEMORY_VAR')           -- By ENV name
assert(v1 == v2 and v2 == v3)                     -- All identical
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
   - Router storage operations
   - All three access modes
   - Cross-verification tests

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

### Three Access Modes Benefits
- **Flexibility**: Multiple ways to access the same variables
- **Consistency**: All access modes return identical values
- **Namespace Support**: Automatic namespace handling
- **Direct Access**: Environment variable name lookup
- **Cross-Context**: Full namespace specification for cross-context access

## Test Cases

The demo includes comprehensive tests for:

1. **All 4 Storage Types**: Memory, File, OS, and Router storage
2. **All 3 Access Modes**: By name, by full name, and by ENV name
3. **Cross-Verification**: Ensuring all access modes return identical values
4. **Default Values**: Proper fallback to default values when storage is empty
5. **Read-Only Enforcement**: OS storage correctly prevents write operations
6. **Router Fallback**: Variables not found in primary storage fall back to secondary storages
7. **Router Write Behavior**: Ensuring writes go to primary storage only
8. **Complex Router Scenarios**: Testing with all underlying storage types
9. **Namespace Isolation**: Variables in different namespaces don't interfere
10. **Error Handling**: Proper handling of missing variables and read-only violations 