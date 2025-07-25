# Loader Module Specification

## Overview

The Loader module provides functionality for loading registry entries from configuration files and directories.
It supports various file formats (JSON, YAML) and includes interpolation capabilities for file inclusion within
configuration files.

## Module Interface

### Loading the Module

The loader module is loaded with:

```lua
local loader = require("loader")
```

### Creating a Loader Instance

```lua
local loader_instance, err = loader.new(filesystem_name)
-- Parameters: 
--   filesystem_name (string) - Name of the filesystem to use for file operations
-- Returns on success: loader instance, nil
-- Returns on error: nil, error message

-- Example:
local loader_instance, err = loader.new("app:local")
if not loader_instance then
  print("Error creating loader: " .. err)
  return
end
```

### Loading Operations

#### Load Entries from Directory

```lua
local entries, err = loader_instance:load_directory(dir_path)
-- Parameters: 
--   dir_path (string) - Path to the directory containing registry configuration files
-- Returns on success: array of entry tables, nil
-- Returns on error: nil, error message

-- Example:
local entries, err = loader_instance:load_directory("/path/to/configs")
if not entries then
  print("Error loading entries: " .. err)
  return
end
```

#### Load Entries from File

```lua
local entries, err = loader_instance:load_file(file_path)
-- Parameters:
--   file_path (string) - Path to the registry configuration file
-- Returns on success: array of entry tables, nil
-- Returns on error: nil, error message

-- Example:
local entries, err = loader_instance:load_file("/path/to/config.yaml")
if not entries then
  print("Error loading file: " .. err)
  return
end
```

## Core Concepts

### Registry Configuration Files

Registry configuration files can be written in JSON or YAML formats and support both single-entry and multi-entry
structures.

#### Single Entry Format

```yaml
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

## Entry Format

Each entry loaded by the loader will have the following structure:

```lua
entry = {
  namespace = "string", -- Namespace of the entry
  name = "string",      -- Name of the entry
  kind = "string",      -- Kind/type of the entry
  meta = {              -- Metadata table (optional)
    [key] = value,      -- Various metadata fields
    ...
  },
  -- Additional fields specific to the entry kind
  ...
}
```

## Complete Example

```lua
-- Import the loader module
local loader = require("loader")

-- Create a loader instance for the "app:local" filesystem
local loader_instance, err = loader.new("app:local")
if not loader_instance then
  print("Error creating loader: " .. err)
  return
end

-- Load all configuration files from a directory
local entries, err = loader_instance:load_directory("/path/to/configs")
if not entries then
  print("Error loading entries: " .. err)
  return
end

print("Loaded " .. #entries .. " entries from directory")

-- Process the loaded entries
for i, entry in ipairs(entries) do
  print(string.format("Entry %d: %s/%s (%s)", i, entry.namespace, entry.name, entry.kind))
  
  -- Example: Process entries based on kind
  if entry.kind == "service" then
    -- Handle service entries
    if entry.endpoints then
      for _, endpoint in ipairs(entry.endpoints) do
        print(string.format("  Endpoint: %s on port %d", endpoint.path, endpoint.port))
      end
    end
  elseif entry.kind == "config" then
    -- Handle configuration entries
    print("  Configuration for: " .. entry.name)
  end
end

-- Example: Alternative approach using a single file
local service_entries, err = loader_instance:load_file("/path/to/services.yaml")
if not service_entries then
  print("Error loading services file: " .. err)
  return
end

print("Loaded " .. #service_entries .. " service entries from file")
```