# Registry Manager

## Overview

The Registry Manager provides a comprehensive system for managing the distributed registry in the Fortress platform. It consists of an agent and a set of tools that enable users to create, read, update, and delete any type of registry entry and manage registry versions.

## Components

- **Registry Manager Agent**: An AI agent specialized in registry operations
- **Registry Management Tools**: Lua functions for interacting with the registry system

## Agent Capabilities

The Registry Manager Agent can:

1. **Get Entry**: Retrieve a specific registry entry by ID
2. **Find Entries**: Search for registry entries using criteria
3. **Create Entry**: Add new entries to the registry
4. **Update Entry**: Modify existing registry entries
5. **Delete Entry**: Remove entries from the registry
6. **List Namespace**: View all entries in a specific namespace
7. **Get Versions**: View the registry version history
8. **Apply Version**: Rollback to a specific registry version
9. **Get Headers**: Retrieve minimal header information (namespace, kind, name, comment) for entries without fetching full metadata or data

## Registry Structure

The registry is a distributed key-value store organized by namespaces. Each registry entry:

- Has a unique ID consisting of a namespace and name (e.g., "fortress.pages:dashboard")
- Has a specific kind (e.g., "registry.entry", "function.lua", "http.endpoint")
- Contains metadata and data components
- Is versioned for history tracking

## Working with Registry Entries

### Entry ID Format

Entry IDs can be specified in two ways:

1. String format: `"namespace:name"`
2. Table format: `{ ns = "namespace", name = "entry-name" }`

### Common Entry Kinds

- `registry.entry`: General-purpose registry entry
- `function.lua`: Lua function code
- `http.endpoint`: HTTP API endpoint definition
- `agent.gen1`: AI agent definition

### Registry Operations

All registry operations follow these steps:

1. Get a snapshot of the registry
2. Create a changeset from the snapshot
3. Apply operations to the changeset
4. Apply the changeset to create a new version

### Finding Entries by Criteria

The registry supports various search patterns:

- Root fields (special prefixes): `.kind`, `.name`
- Metadata field matching operators:
  - Standard equality match: `field`
  - Regex pattern match: `~field`
  - Contains match: `*field`
  - Prefix match: `^field`
  - Suffix match: `$field`

Example criteria:
```lua
{
  [".kind"] = "registry.entry",
  ["meta.type"] = "virtual.page"
}
```

## Version Control

The registry maintains a complete history of all changes. You can:

- View the version history
- Get a snapshot at a specific version
- Roll back to a previous version

## Technical Notes

- Registry operations are performed on snapshots to provide consistency
- Changes are accumulated in a changeset before being applied atomically
- Different entry kinds have different schema requirements for metadata and data
- Be careful when modifying system entries - always understand the potential impacts
- Consider version control implications when making changes