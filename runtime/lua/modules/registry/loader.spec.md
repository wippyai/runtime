# Loader Module Specification

## Overview

The Loader module provides functionality for loading registry entries from configuration files and directories.
It supports various file formats (JSON, YAML) and includes interpolation capabilities for variable substitution within
configuration files.

## Module Interface

### Loading the Module

```lua
local loader = require("loader")
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
local loader = require("loader")("filesystem_name")
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
local loader = require("loader")("local")
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
local loader = require("loader")("local")
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
  print(i .. ". " .. entry.namespace .. "/" .. entry.name .. " (" .. entry.kind .. ")")
  
  -- Access metadata
  if entry.meta and entry.meta.environment then
    print("   Environment: " .. entry.meta.environment)
  end
  
  -- Access data
  if entry.data then
    -- Process entry data...
  end
end
```

### Complete Workflow: Load and Process Entries

```lua
-- Import the fs and loader modules
local fs = require("fs")
local loader = require("loader")("local")
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

-- Example: Save processed entries to a new file
local function save_processed_entries(entries, outputPath)
  local content = ""
  for _, entry in ipairs(entries) do
    content = content .. string.format("# %s/%s (%s)\n", entry.namespace, entry.name, entry.kind)
    -- Add more formatting as needed
  end
  
  local fsys = fs.get("local")
  fsys:writefile(outputPath, content)
  print("Saved processed entries to: " .. outputPath)
end

save_processed_entries(entries, "/path/to/output.txt")
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