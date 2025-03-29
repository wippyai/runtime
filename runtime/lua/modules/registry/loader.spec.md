# Registry Loader Module Specification

## Overview

The Registry Loader module provides functionality for loading registry entries from configuration files and directories.
It supports various file formats (JSON, YAML) and includes interpolation capabilities for variable substitution within
configuration files.

## Module Interface

### Loading the Module

```lua
local registry = require("registry")
local loader = registry.loader("filesystem_name")
```

The loader module is a submodule of the registry module and needs to be initialized with a filesystem name that will be
used for file operations.

## Core Concepts

### Registry Configuration Files

Registry configuration files can be written in JSON or YAML formats and support both single-entry and multi-entry
structures.

#### Single Entry Format

```yaml
id: "namespace:name"
# OR
namespace: services
name: api-gateway
kind: service
meta:
  version: "1.0"
  owner: "platform-team"
  environment: "production"
# Additional data fields
endpoints:
  - path: /api/v1
    port: 8080
```

#### Multi-Entry Format

```yaml
namespace: services
meta:
  # Shared metadata for all entries
  owner: "platform-team"
  environment: "production"
entries:
  - name: api-gateway
    kind: service
    meta:
      version: "1.0"
    # Entry-specific data
    endpoints:
      - path: /api/v1
        port: 8080

  - name: auth-service
    kind: service
    meta:
      version: "2.0"
    # Entry-specific data
    endpoints:
      - path: /auth
        port: 8081
```

### Variable Interpolation

Configuration files support variable interpolation using `${variable}` syntax:

```yaml
namespace: ${env}-services
name: api-gateway
kind: service
meta:
  environment: ${env}
  region: ${region}
```

### File Inclusion

Configuration files can include content from other files using the `file://` protocol:

```yaml
namespace: services
name: api-gateway
kind: service
meta:
  version: "1.0"
# Include configuration from another file
config: file:///path/to/config.json
```

## Loading Operations

### Create Loader Instance

```lua
local loader = require("registry").loader("filesystem_name")
-- Parameters: 
--   filesystem_name (string) - Name of the filesystem to use for file operations
-- Returns on success: loader instance, nil
-- Returns on error: nil, error message
```

### Load Entries from Directory

```lua
local entries, err = loader:load_directory(dirPath, variables)
-- Parameters: 
--   dirPath (string) - Path to the directory containing registry configuration files
--   variables (table, optional) - Variables for interpolation
-- Returns on success: array of entry tables, nil
-- Returns on error: nil, error message
```

### Load Entries from File

```lua
local entries, err = loader:load_file(filePath, variables)
-- Parameters:
--   filePath (string) - Path to the registry configuration file
--   variables (table, optional) - Variables for interpolation
-- Returns on success: array of entry tables, nil
-- Returns on error: nil, error message
```

## Example Usage

### Basic File Loading

```lua
-- Import the registry module
local registry = require("registry")

-- Create a loader instance for the "local" filesystem
local loader = registry.loader("local")
if not loader then
  print("Error creating loader")
  return
end

-- Load entries from a single YAML file
local entries, err = loader:load_file("/path/to/config.yaml")
if not entries then
  print("Error loading file: " .. err)
  return
end

print("Loaded " .. #entries .. " entries from file")
```

### Directory Loading with Variable Interpolation

```lua
-- Import the registry module
local registry = require("registry")

-- Create a loader instance
local loader = registry.loader("local")
if not loader then
  print("Error creating loader")
  return
end

-- Define variables for interpolation
local variables = {
  env = "production",
  region = "us-west-2",
  domain = "example.com"
}

-- Load all configuration files from a directory with variable interpolation
local entries, err = loader:load_directory("/path/to/configs", variables)
if not entries then
  print("Error loading entries: " .. err)
  return
end

print("Loaded " .. #entries .. " entries from directory")

-- Process the loaded entries
for i, entry in ipairs(entries) do
  print(i .. ". " .. entry.id .. " (" .. entry.kind .. ")")
  
  -- Access metadata
  if entry.meta.environment then
    print("   Environment: " .. entry.meta.environment)
  end
  
  -- Access data
  if entry.data then
    -- Process entry data...
  end
end
```

### Complete Workflow: Load, Transform, and Apply

```lua
-- Import the registry module
local registry = require("registry")

-- Create a loader instance
local loader = registry.loader("local")
if not loader then
  print("Error creating loader")
  return
end

-- Define variables for interpolation
local variables = {
  env = "production",
  region = "us-west-2"
}

-- Load entries from a directory
local entries, err = loader:load_directory("/path/to/configs", variables)
if not entries then
  print("Error loading entries: " .. err)
  return
end
print("Loaded " .. #entries .. " entries from directory")

-- Get current registry state
local snapshot, err = registry.snapshot()
if not snapshot then
  print("Error getting snapshot: " .. err)
  return
end
local currentEntries = snapshot:entries()

-- Build delta between current state and loaded entries
local changeset, err = registry.build_delta(currentEntries, entries)
if not changeset then
  print("Error building delta: " .. err)
  return
end

print("Delta contains " .. #changeset .. " operations")

-- Apply changes to registry
local changes = snapshot:changes()
for _, op in ipairs(changeset) do
  if op.kind == "create" then
    changes:create(op.entry)
    print("Adding: " .. op.entry.id)
  elseif op.kind == "update" then
    changes:update(op.entry)
    print("Updating: " .. op.entry.id)
  elseif op.kind == "delete" then
    changes:delete(op.entry.id)
    print("Removing: " .. op.entry.id)
  end
end

-- Apply changes to create a new version
local version, err = changes:apply()
if version then
  print("Updated registry to version: " .. version:id())
else
  print("Error applying changes: " .. err)
end
```