# Registry Package Specification

## Overview

The Registry package provides a Lua interface for working with a distributed registry system. It offers operations for querying, creating, updating, and deleting registry entries, as well as version history management capabilities.

## Module Interface

### Loading the Module

```lua
local registry = require("registry")
```

## Core Concepts

### Registry Entries

Each registry entry consists of:
- **ID**: A composite identifier with namespace and name components
- **Kind**: Type classifier for the entry
- **Metadata**: A table of key-value pairs containing additional attributes
- **Data**: Arbitrary payload attached to the entry

### Snapshots

A snapshot represents a point-in-time view of the registry's state. Most operations are performed on snapshots to provide consistency.

### Versioning

All changes to the registry are versioned. The registry maintains a complete history of changes that can be traversed and applied.

### Changes API

Changes to the registry are accumulated in a changeset before being applied atomically to create a new version.

## Basic Operations

### Get Current Snapshot

```lua
local snapshot, err = registry.snapshot()
-- Returns on success: snapshot object, nil
-- Returns on error: nil, error message
```

### Get Specific Entry

```lua
local entry, err = snapshot:get("namespace:name")
-- Parameters: id (string) - Entry ID in "namespace:name" format
-- Returns on success: entry table, nil
-- Returns on error: nil, error message

-- Entry Structure:
-- entry.id: id (string) - Entry ID in "namespace:name" format
-- entry.kind: Entry kind (string)
-- entry.meta: Metadata table (key-value pairs)
-- entry.data: Entry data (any value)
```

### List Entries by Namespace

```lua
local entries = snapshot:namespace("services")
-- Parameters: namespace (string) - Namespace to filter by
-- Returns: Array of entry tables
```

### List All Entries

```lua
local allEntries = snapshot:entries({limit = 100, offset = 0})
-- Parameters: options (table, optional)
--   options.limit: Maximum number of entries to return
--   options.offset: Offset for pagination
-- Returns: Array of entry tables
```

## Searching & Filtering

### Find Entries by Criteria

```lua
local entries = snapshot:find(criteria)
-- Parameters: criteria (table) - Search criteria
-- Returns: Array of matching entry tables
```

The search criteria supports various matching patterns:

#### Root Fields (Special Prefixes):
- `.kind`: Match entry's Kind field (exact match)
- `.name`: Match entry's ID.Name field (exact match)

#### Metadata Field Matching Operators:
- `field`: Standard equality match for the field
- `~field`: Regex pattern match (e.g., "~description": ".*service.*")
- `*field`: Contains match (substring search)
- `^field`: Prefix match (starts with)
- `$field`: Suffix match (ends with)

#### Examples:

```lua
-- Find all production services in the us-west region
local productionServices = snapshot:find({
  kind = "service",
  meta = {
    environment = "production",
    ["*region"] = "us-west"
  }
})

-- Find all primary databases in services or infra namespaces
local databaseEntries = registry.find({
  kind = "database",
  ["~namespace"] = "^(services|infra)$",
  meta = {
    tier = "primary"
  }
})
```

## Making Changes

### Create Changeset

```lua
local changes = snapshot:changes()
-- Returns: changeset object
```

### Create Entry

```lua
changes:create({
  id = { ns = "namespace", name = "entry-name" },
  kind = "entry-kind",
  meta = {
    -- Metadata key-value pairs
    environment = "production",
    owner = "team-name",
    tags = {"tag1", "tag2"}
  },
  data = {
    -- Any data to associate with the entry
  }
})
-- Returns: changeset object (for chaining)
```

### Update Entry

```lua
changes:update({
  id = { ns = "namespace", name = "entry-name" },
  kind = "entry-kind",
  meta = {
    -- Updated metadata
  },
  data = {
    -- Updated data
  }
})
-- Returns: changeset object (for chaining)
```

### Delete Entry

```lua
-- Using string ID
changes:delete("namespace:entry-name")

-- Or using table ID
changes:delete({ns = "namespace", name = "entry-name"})
-- Returns: changeset object (for chaining)
```

### Apply Changes

```lua
local version, err = changes:apply()
-- Returns on success: version object, nil
-- Returns on error: nil, error message
```

## Version History

### Get History Object

```lua
local history = registry.history()
-- Returns: history object
```

### Get Current Version

```lua
local currentVersion = registry.current_version()
-- Returns: version object
```

### List All Versions

```lua
local versions, err = history:versions()
-- Returns on success: array of version objects, nil
-- Returns on error: nil, error message
```

### Get Specific Version

```lua
local specificVersion, err = history:get_version(42)
-- Parameters: version_id (number) - Version ID to retrieve
-- Returns on success: version object, nil
-- Returns on error: nil, error message
```

### Get Snapshot at Specific Version

```lua
local oldSnapshot, err = history:snapshot_at(42)
-- Parameters: version_id (number) - Version ID for the snapshot
-- Returns on success: snapshot object, nil
-- Returns on error: nil, error message
```

### Apply Specific Version (Rollback)

```lua
local success, err = registry.apply_version(specificVersion)
-- Parameters: version (version object) - Version to apply
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

## Version Object Operations

### Get Version ID

```lua
local id = version:id()
-- Returns: version ID (number)
```

### Get Previous Version

```lua
local previousVersion = version:previous()
-- Returns: previous version object or nil if this is the first version
```

### Get Version String Representation

```lua
local versionString = version:string()
-- Returns: string representation of the version
```

## Utility Functions

### Parse ID String

```lua
local id = registry.parse_id("namespace:name")
-- Parameters: id_string (string) - ID in "namespace:name" format
-- Returns: ID table with {ns = "namespace", name = "name"}
```

### Get Snapshot Version

```lua
local version = snapshot:version()
-- Returns: version object for the snapshot
```

## Example Usage

### Basic Operations

```lua
-- Import the registry module
local registry = require("registry")

-- Get the current snapshot
local snapshot, err = registry.snapshot()
if not snapshot then
  print("Error getting snapshot: " .. err)
  return
end

-- Get a specific entry
local entry, err = snapshot:get("services:database")
if entry then
  print("Found entry: " .. entry.id.ns .. ":" .. entry.id.name)
  print("Kind: " .. entry.kind)
  print("Environment: " .. (entry.meta.environment or "not set"))
else
  print("Entry not found: " .. (err or "unknown error"))
end

-- Get all entries in a namespace
local services = snapshot:namespace("services")
print("Found " .. #services .. " services")
```

### Advanced Searching

```lua
-- Find entries by criteria using a snapshot
local productionServices = snapshot:find({
  kind = "service",
  meta = {
    environment = "production",
    ["*region"] = "us-west"
  }
})
print("Found " .. #productionServices .. " production services in us-west")

-- Use regex pattern matching to find entries
local apiServices = snapshot:find({
  ["~kind"] = "service",
  meta = {
    ["~name"] = ".*api.*",
    ["^status"] = "healthy"
  }
})
print("Found " .. #apiServices .. " healthy API services")
```

### Making Changes

```lua
-- Create a changeset from the snapshot
local changes = snapshot:changes()

-- Create a new entry
changes:create({
  id = { ns = "services", name = "new-service" },
  kind = "service",
  meta = {
    environment = "staging",
    owner = "platform-team",
    tags = {"microservice", "api"}
  },
  data = {
    port = 8080,
    limits = {memory = "1Gi", cpu = "0.5"}
  }
})

-- Update an existing entry
changes:update({
  id = { ns = "config", name = "rate-limits" },
  kind = "config",
  meta = {
    updated = os.time(),
    revision = 3
  },
  data = {
    rate = 100,
    burst = 200
  }
})

-- Delete an entry
changes:delete("services:deprecated-service")

-- Apply changes to create a new version
local version, err = changes:apply()
if version then
  print("Created version: " .. version:id())
else
  print("Error applying changes: " .. err)
end
```

### Working with Version History

```lua
-- Get history object
local history = registry.history()

-- Get current version
local currentVersion = registry.current_version()
print("Current version: " .. currentVersion:id())

-- List all versions
local versions, err = history:versions()
for i, ver in ipairs(versions) do
  print(i .. ". Version: " .. ver:id() .. " - " .. ver:string())
  
  -- Navigate version chain
  local prev = ver:previous()
  if prev then
    print("   Previous: " .. prev:id())
  end
end

-- Get a snapshot at a specific version
local oldSnapshot, err = history:snapshot_at(42)
if oldSnapshot then
  local entriesAtVersion = oldSnapshot:entries()
  print("Found " .. #entriesAtVersion .. " entries at version 42")
  
  -- Search within the historical snapshot
  local oldServices = oldSnapshot:find({kind = "service"})
  print("Found " .. #oldServices .. " services at version 42")
end

-- Apply a specific version (rollback)
local success, err = registry.apply_version(specificVersion)
if success then
  print("Successfully rolled back to version " .. specificVersion:id())
else
  print("Error rolling back: " .. err)
end
```