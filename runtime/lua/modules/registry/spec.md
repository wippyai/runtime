<!-- SPDX-License-Identifier: MPL-2.0 -->

# registry

Registry operations for entries, snapshots, and versioning. Storage, nondeterministic.

## Loading

```lua
local registry = require("registry")
```

## Functions

### parse_id(id_str: string) → table

Parses a registry ID string into namespace and name components.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id_str | string | yes | - | Format: "namespace:name" or just "name" |

**Returns:** Table with fields `{ns: string, name: string}`

- If format is "namespace:name", splits on first colon
- If no colon, ns is empty string and name is full string

```lua
local id = registry.parse_id("app.lib:assert")
-- id.ns = "app.lib"
-- id.name = "assert"
```

### get(id: string) → table, error

Retrieves a single entry by ID from the current registry state.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Entry ID in format "namespace:name" |

**Returns:**

- Success: Entry table with fields `{id: string, kind: string, meta: table, data: any, root: boolean}`, nil
- Error: nil, structured error

`root` marks an `ns.dependency` selected as a deployment root. It is the sole
authority for that status — `meta` is user space and carries no trust. Reads
always return it. Updates preserve the stored value when `root` is omitted; set
it explicitly only when changing root status.

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| Entry not found | errors.NOT_FOUND | no |
| Conversion failed | errors.INTERNAL | no |

### find(filter: table) → table[], error

Searches for entries matching filter criteria.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| filter | table | yes | - | Search criteria with metadata fields |

**filter fields:**

| Field | Type | Notes |
|-------|------|-------|
| kind | string | Filter by entry kind |
| type | string | Filter by meta.type field |
| meta | table | Nested metadata filters |
| (any) | any | Any other field filters metadata |

**Returns:**

- Success: Array of entry tables (may be empty), nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| Finder not available | errors.INTERNAL | no |
| Find operation failed | errors.INTERNAL | no |
| Conversion failed | errors.INTERNAL | no |

```lua
local entries, err = registry.find({kind = "function.lua"})
local tests, err = registry.find({type = "test"})
```

### snapshot() → Snapshot, error

Creates a snapshot of the current registry state.

**Returns:**

- Success: Snapshot object, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| Get current version failed | errors.INTERNAL | no |
| Get registry entries failed | errors.INTERNAL | no |

### snapshot_at(version_id: integer) → Snapshot, error

Creates a snapshot of the registry at a specific version.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| version_id | integer | yes | - | Must be > 0 |

**Returns:**

- Success: Snapshot object, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| Invalid version ID (≤ 0) | errors.INVALID | no |
| History not available | errors.INTERNAL | no |
| Get versions failed | errors.INTERNAL | no |
| Version not found | errors.NOT_FOUND | no |
| Build snapshot state failed | errors.INTERNAL | no |

### overlay(id: string) → Snapshot, error

Opens the process-local registry overlay owned by `id`. It returns the regular
`Snapshot` type, so callers use the existing `snapshot:changes()` and
`changes:apply()` workflow. Overlay entries participate in normal registry
topology and resource lifecycle, but never create a history version. They
survive ordinary history commits and version selection, and are cleared by a
cold boot or explicit state load; the owning controller must reconcile them.

Applying changes uses generation-based optimistic concurrency. A stale snapshot
fails atomically with a retryable `errors.CONFLICT` and must be reopened. Owner
IDs are trimmed to one canonical identity before authorization and storage. One
changeset may contain at most one operation for each entry ID.

Ownership is registry state, not entry metadata. Overlay application never adds
an owner field to `meta` or otherwise changes user metadata. Kinds handled by a
registry expansion directive are rejected because their generated entries and
effects do not have process-local ownership semantics.

```lua
local live, err = registry.overlay("controllers:customer-db")
if err then return nil, err end

local changes = live:changes()
changes:create({
    id = "runtime.data_sources:customer-db",
    kind = "db.sql.postgres",
    data = { host = "db.example.com", database = "customer" },
})
local version, apply_err = changes:apply()

-- Deprovision every entry in this overlay in one dependency-sorted apply.
local current, open_err = registry.overlay("controllers:customer-db")
if open_err then return nil, open_err end
local remove = current:changes()
remove:delete(current:entries())
local _, delete_err = remove:apply()
```

Authorization is checked when the overlay is opened and again on every
snapshot operation and apply. Required actions are:

- `registry.overlay.get` on the overlay ID
- `registry.overlay.apply` on the overlay ID
- `registry.overlay.create.<kind>`, `registry.overlay.update.<kind>`, or
  `registry.overlay.delete.<kind>` on each real entry ID

This second, per-entry check prevents a controller authorized for one overlay
from creating arbitrary kinds or writing outside its allowed namespaces.

The overlay ID is a logical, policy-controlled owner, not the caller's process
ID. This lets a boot reconciler reopen resources created by an earlier control
invocation. Grant exact or namespace-scoped owner policies deliberately; a
wildcard owner grant authorizes that controller to manage every matching
overlay. Ordinary `registry.get`, `find`, and `snapshot` read the composed
effective registry and continue to require their existing per-entry
`registry.get` permission—they do not use the owner-level overlay permission.

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Empty owner | errors.INVALID | no |
| Owner read/apply denied | errors.PERMISSION_DENIED | no |
| Entry operation denied | errors.PERMISSION_DENIED | no |
| Stale overlay generation | errors.CONFLICT | yes |
| Directive-owned kind | errors.INVALID | no |

### current_version() → Version, error

Returns the current registry version.

**Returns:**

- Success: Version object, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| Get current version failed | errors.INTERNAL | no |

### versions() → Version[], error

Returns all available versions in the registry history.

**Returns:**

- Success: Array of Version objects, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| History not available | errors.INTERNAL | no |
| Get versions failed | errors.INTERNAL | no |

### history() → History, error

Returns a History object for accessing registry version history.

**Returns:**

- Success: History object, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Registry not in context | errors.INTERNAL | no |
| History not available | errors.INTERNAL | no |

### apply_version(version: Version) → boolean, error

Applies a specific version to the registry, effectively rolling back or forward.
This operation **re-runs registry directives/effects** on the version changeset,
so module baseline entries are rebuilt consistently with the dependency set.
Directive implementations should be deterministic and avoid non-essential I/O.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| version | Version | yes | - | Version object to apply |

**Returns:**

- Success: true, nil
- Error: false, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No context available | errors.INTERNAL | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| Registry not in context | errors.INTERNAL | no |
| Invalid version object | errors.INVALID | no |
| Apply version failed | errors.INTERNAL | no |

### build_delta(from_entries: table[], to_entries: table[]) → table[], error

Computes the operations needed to transition from one entry set to another.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| from_entries | table[] | yes | - | Array of entry tables (starting state) |
| to_entries | table[] | yes | - | Array of entry tables (target state) |

**Returns:**

- Success: Array of operation tables `{kind: string, entry: table}`, nil
- Error: nil, structured error

Operation kinds: "entry.create", "entry.update", "entry.delete"

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| from_entries not table | errors.INVALID | no |
| to_entries not table | errors.INVALID | no |
| Transcoder not available | errors.INTERNAL | no |
| Build delta failed | errors.INTERNAL | no |
| Conversion failed | errors.INTERNAL | no |

## Types

### Snapshot

Returned by `registry.snapshot()`, `registry.snapshot_at()`, and `history:snapshot_at()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| entries | () | table[], error | All entries in snapshot |
| get | (id: string) | table, error | Single entry by ID |
| namespace | (ns: string) | table[] | Entries in namespace |
| find | (filter: table) | table[] | Search entries |
| changes | () | Changes | Create changeset |
| version | () | Version | Snapshot version |

#### snapshot:entries() → table[], error

Returns all entries in the snapshot.

**Returns:**

- Success: Array of entry tables, nil
- Error: nil, structured error

Entries are filtered by security permissions.

#### snapshot:get(id: string) → table, error

Retrieves a specific entry by ID from the snapshot.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Entry ID |

**Returns:**

- Success: Entry table, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| Entry not found | errors.NOT_FOUND | no |
| Conversion failed | errors.INTERNAL | no |

#### snapshot:namespace(ns: string) → table[]

Returns all entries in a specific namespace.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ns | string | yes | - | Namespace to filter |

**Returns:** Array of entry tables (filtered by permissions)

#### snapshot:find(filter: table) → table[]

Searches entries in snapshot matching filter criteria.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| filter | table | yes | - | Search criteria |

**Returns:** Array of entry tables matching filter

#### snapshot:changes() → Changes

Creates a Changes object for building a changeset.

**Returns:** Changes object for modification operations

#### snapshot:version() → Version

Returns the version of this snapshot.

**Returns:** Version object

### Changes

Returned by `snapshot:changes()`. Used to build and apply changesets.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| ops | () | table[] | List of operations |
| create | (entry: table) | Changes | Add create operation |
| update | (entry: table) | Changes | Add update operation |
| delete | (id or id[]) | Changes | Add one or more delete operations |
| apply | () | Version, error | Apply changes |

#### changes:ops() → table[]

Returns the list of operations in the changeset.

**Returns:** Array of operation tables `{kind: string, entry: table}`

Operation kinds: "entry.create", "entry.update", "entry.delete"

#### changes:create(entry: table) → Changes

Adds a create operation to the changeset.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| entry | table | yes | - | Entry table with id, kind, meta, data |

**Returns:** Changes object (for chaining)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Invalid entry format | errors.INVALID | no |
| Conversion failed | errors.INVALID | no |

#### changes:update(entry: table) → Changes

Adds an update operation to the changeset.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| entry | table | yes | - | Entry table with id, kind, meta, data |

**Returns:** Changes object (for chaining)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Invalid entry format | errors.INVALID | no |
| Conversion failed | errors.INVALID | no |

#### changes:delete(id_or_ids: string | table | table[]) → Changes

Adds a delete operation to the changeset.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id_or_ids | string \| table \| table[] | yes | - | ID, entry with `id`, ns/name table, or nested list of these |

**Returns:** Changes object (for chaining)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Invalid ID format | errors.INVALID | no |

```lua
-- String ID
changes:delete("app.test:example")

-- Table ID
changes:delete({ns = "app.test", name = "example"})

-- Bulk delete (duplicates are ignored)
changes:delete(snapshot:entries())
```

#### changes:apply() → Version, error

Applies the changeset. A normal snapshot creates a registry version. An overlay
snapshot changes only its process-local overlay and returns the unchanged
current durable version.

**Returns:**

- Success: New Version object, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No changes to apply | errors.INVALID | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| Sort operations failed | errors.INTERNAL | no |
| Apply changes failed | errors.INTERNAL | no |

### Version

Returned by version-related functions. Represents a specific registry version.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| id | () | integer | Version ID number |
| previous | () | Version \| nil | Previous version |
| string | () | string | String representation |

#### version:id() → integer

Returns the numeric ID of the version.

**Returns:** Integer version ID (may be 0 for initial version)

#### version:previous() → Version | nil

Returns the previous version in the history chain.

**Returns:** Version object or nil if this is the first version

#### version:string() → string

Returns a string representation of the version.

**Returns:** String describing the version

### History

Returned by `registry.history()`. Provides access to version history.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| versions | () | Version[], error | All versions |
| get_version | (id: integer) | Version, error | Version by ID |
| snapshot_at | (version: Version) | Snapshot | Snapshot at version |

#### history:versions() → Version[], error

Returns all available versions in the history.

**Returns:**

- Success: Array of Version objects, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Get versions failed | errors.INTERNAL | no |

#### history:get_version(id: integer) → Version, error

Retrieves a specific version by its ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | integer | yes | - | Version ID (must be >= 0) |

**Returns:**

- Success: Version object, nil
- Error: nil, structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Invalid version ID (< 0) | errors.INVALID | no |
| Get versions failed | errors.INTERNAL | no |
| Version not found | errors.NOT_FOUND | no |

#### history:snapshot_at(version: Version) → Snapshot

Creates a snapshot at a specific version.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| version | Version | yes | - | Version object |

**Returns:** Snapshot object at the specified version

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Invalid version object | errors.INVALID | no |
| Build snapshot state failed | errors.INTERNAL | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local entry, err = registry.get("app:example")
if err then
    if err:kind() == errors.NOT_FOUND then
        -- entry doesn't exist
    elseif err:kind() == errors.PERMISSION_DENIED then
        -- access denied
    elseif err:kind() == errors.INTERNAL then
        -- internal error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.PERMISSION_DENIED`, `errors.INTERNAL`

## Example

```lua
local registry = require("registry")

-- Get an entry
local entry, err = registry.get("app.lib:assert")
if err then error(err) end

print(entry.id)    -- "app.lib:assert"
print(entry.kind)  -- "function.lua"

-- Parse ID
local id = registry.parse_id(entry.id)
print(id.ns, id.name)  -- "app.lib" "assert"

-- Find entries
local entries, err = registry.find({kind = "function.lua"})
if err then error(err) end

-- Work with snapshots and versions
local snap, err = registry.snapshot()
if err then error(err) end

local version = snap:version()
print("Version ID:", version:id())

-- Navigate versions
local prev = version:previous()
if prev then
    print("Previous version:", prev:id())
end

-- Create changes
local changes = snap:changes()
changes:create({
    id = "test:example",
    kind = "test.kind",
    meta = {type = "test"},
    data = {value = 123}
})

local new_version, err = changes:apply()
if err then error(err) end

-- Rollback to previous version
local ok, err = registry.apply_version(version)
if err then error(err) end
```
