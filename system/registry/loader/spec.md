# Registry Loader Specification

## Overview

The Registry Loader is responsible for loading registry entries from configuration files and directories.
It supports various file formats (JSON, YAML) and includes interpolation capabilities for file inclusion within
configuration files.

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

## 6. State Management

### 6.1 Operations

- Create: First-time component creation
- Update: Modify existing component
- Delete: Remove component

### 6.2 Version Control

- Linear version history
- Immutable versions
- Atomic changes
- Full rollback support

### 6.3 State Transitions

- Each change creates new version
- Changes are atomic
- State is always consistent
- Failed changes are rolled back

## 7. Validation Rules

### 7.1 Required Elements

- namespace - Component namespace
- name - Component identifier
- kind - Component type

### 7.2 Reference Validation

- Must be fully qualified
- Must exist at runtime
- Must not create cycles
- Must resolve to valid target

## 8. Best Practices

### 8.1 Organization

- Group related components
- Use consistent naming
- Keep configurations focused
- Document structure

### 8.2 Dependencies

- Minimize cross-namespace dependencies
- Use explicit dependencies
- Avoid deep dependency chains
- Document external dependencies

### 8.3 Security

- No secrets in files
- Use environment for sensitive data
- Validate all inputs
- Restrict access appropriately

### 8.4 Versioning

- Version your components
- Use semantic versioning
- Document changes
- Test transitions