-- Import the registry module
local registry = require("registry")

-- BASIC OPERATIONS
-- ----------------

-- Get the current snapshot of the registry
local snapshot, err = registry.snapshot()
if not snapshot then
print("Error getting snapshot: " .. err)
return
end

-- Get a specific entry by ID
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

-- List all entries with pagination
local allEntries = snapshot:entries({limit = 100, offset = 0})
print("Registry has " .. #allEntries .. " entries")

-- SEARCHING & FILTERING
-- ---------------------

-- Find entries by criteria using a snapshot
local productionServices = snapshot:find({
kind = "service",
meta = {
environment = "production",
region_contains = "us-west"
}
})
print("Found " .. #productionServices .. " production services in us-west")

-- Direct find on registry (same semantics)
local databaseEntries = registry.find({
kind = "database",
namespace_pattern = "^(services|infra)$",
meta = {
tier = "primary"
}
})
print("Found " .. #databaseEntries .. " primary databases")

-- MAKING CHANGES
-- --------------

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
-- Alternative syntax
changes:delete({ns = "services", name = "another-deprecated"})

-- Apply changes to create a new version
local version, err = changes:apply()
if version then
print("Created version: " .. version:id())
else
print("Error applying changes: " .. err)
end

-- VERSION HISTORY
-- --------------

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

-- Get a specific version
local specificVersion, err = history:get_version(42)
if specificVersion then
print("Got version: " .. specificVersion:string())
end

-- Get a snapshot at a specific version
local oldSnapshot, err = history:snapshot_at(42)
if oldSnapshot then
local entriesAtVersion = oldSnapshot:entries()
print("Found " .. #entriesAtVersion .. " entries at version 42")

-- All the normal snapshot operations work on historical snapshots
local oldServices = oldSnapshot:namespace("services")
local oldEntry = oldSnapshot:get("services:api-gateway")
local filteredOld = oldSnapshot:find({kind = "service"})
end

-- Apply a specific version (rollback)
local success, err = registry.apply_version(specificVersion)
if success then
print("Successfully rolled back to version " .. specificVersion:id())
else
print("Error rolling back: " .. err)
end

-- UTILITY FUNCTIONS
-- ----------------

-- Parse an ID string
local id = registry.parse_id("namespace:name")
print(id.ns .. " - " .. id.name)

-- Get snapshot version
local snapshotVersion = snapshot:version()
print("Snapshot is at version: " .. snapshotVersion:id())