--[[
Migration Runner
---------------

This module provides a high-level API for discovering and executing migrations
from the registry in an isolated manner. It handles finding migrations for a specific
database, running them forward (up) or rolling them back (down).

Usage:
local runner = require("runner")

-- Initialize runner for a specific database
local db_runner = runner.setup("my_database_id")

-- Run all pending migrations
local results = db_runner:run({
    tags = {"schema", "data"}  -- Optional: Filter by tags
})

-- Run just the next pending migration
local results = db_runner:run_next()

-- Roll back the last applied migration
local rollback_result = db_runner:rollback()
]]

local sql = require("sql")
local time = require("time")
local funcs = require("funcs")
local repository = require("repository")
local registry_finder = require("registry")

local runner = {}

-- Runner object for a specific database
local Runner = {}
Runner.__index = Runner

-- Set up a runner for a specific database
function runner.setup(database_id)
    if not database_id then
        error("Database ID is required for migration runner setup")
    end

    local self = setmetatable({}, Runner)
    self.database_id = database_id

    return self
end

-- Find migrations for this database
function Runner:find_migrations(options)
    options = options or {}

    -- Get database connection to determine type
    local db, err = sql.get(self.database_id)
    if err then
        return nil, "Failed to connect to database: " .. err
    end

    -- Get database type
    local db_type, type_err = db:type()
    if type_err then
        db:release()
        return nil, "Failed to determine database type: " .. type_err
    end

    -- Initialize tracking table
    local _, init_err = repository.init_tracking_table(db)
    if init_err then
        db:release()
        return nil, "Failed to initialize migration tracking table: " .. init_err
    end

    -- Get applied migrations
    local applied_migrations, applied_err = repository.get_migrations(db)
    if applied_err then
        db:release()
        return nil, "Failed to get applied migrations: " .. applied_err
    end

    -- Build map of applied migrations for quick lookup
    local applied_map = {}
    for _, m in ipairs(applied_migrations or {}) do
        applied_map[m.id] = m
    end

    -- Find migrations in registry
    local find_options = {
        target_db = self.database_id,
        tags = options.tags
    }

    local migrations, find_err = registry_finder.find(find_options)
    if find_err then
        db:release()
        return nil, "Failed to find migrations: " .. find_err
    end

    db:release()

    -- Enrich migrations with applied status
    for i, migration in ipairs(migrations) do
        migrations[i].applied = applied_map[migration.id] ~= nil
        migrations[i].applied_at = applied_map[migration.id] and applied_map[migration.id].applied_at or nil
    end

    return migrations
end

-- Get the next pending migration
function Runner:get_next_migration(options)
    local migrations, err = self:find_migrations(options)
    if err then
        return nil, err
    end

    if not migrations or #migrations == 0 then
        return nil, "No migrations found"
    end

    -- Migrations are already sorted by timestamp
    -- Find the first one that hasn't been applied yet
    for _, migration in ipairs(migrations) do
        if not migration.applied then
            return migration
        end
    end

    return nil, "All migrations have been applied"
end

-- Execute a single migration with proper settings
local function execute_migration(migration_id, options)
    -- Create function executor
    local executor = funcs.new()

    -- Execute the migration in isolation
    local cmd, exec_err = executor:sync(migration_id, options)

    if exec_err then
        return {
            status = "error",
            error = "Failed to execute migration: " .. exec_err
        }
    end

    return cmd:response()
end

-- Run migrations in isolated manner
function Runner:run(options)
    options = options or {}

    -- Find available migrations
    local migrations, find_err = self:find_migrations(options)
    if find_err then
        return {
            status = "error",
            error = find_err
        }
    end

    if not migrations or #migrations == 0 then
        return {
            status = "complete",
            message = "No migrations found",
            migrations_found = 0,
            migrations_applied = 0,
            migrations_skipped = 0,
            migrations_failed = 0
        }
    end

    -- Track results
    local results = {
        status = "running",
        migrations_found = #migrations,
        migrations_applied = 0,
        migrations_skipped = 0,
        migrations_failed = 0,
        migrations = {}
    }

    -- Start timing
    local start_time = time.now()

    -- Execute each migration that's not already applied
    for _, migration in ipairs(migrations) do
        -- Skip if already applied
        if migration.applied then
            results.migrations_skipped = results.migrations_skipped + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "skipped",
                reason = "Already applied",
                applied_at = migration.applied_at,
                description = migration.meta and migration.meta.description or ""
            })
            goto continue
        end

        -- Set up migration options
        local migration_options = {
            database_id = self.database_id,
            direction = "up"
        }

        -- Execute the migration
        local result = execute_migration(migration.id, migration_options)

        if result and result.status == "error" then
            results.migrations_failed = results.migrations_failed + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "error",
                error = result.error,
                description = migration.meta and migration.meta.description or ""
            })

            -- Stop on first error
            results.status = "error"
            results.error = result.error
            break
        elseif result and result.status == "applied" then
            results.migrations_applied = results.migrations_applied + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "applied",
                description = migration.meta and migration.meta.description or "",
                duration = result.duration
            })
        else
            results.migrations_skipped = results.migrations_skipped + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "skipped",
                reason = result and result.reason or "Unknown",
                description = migration.meta and migration.meta.description or ""
            })
        end

        ::continue::
    end

    local end_time = time.now()
    results.duration = end_time:sub(start_time):milliseconds() / 1000 -- Convert to seconds

    -- Determine final status
    if results.status ~= "error" then
        results.status = "complete"
    end

    return results
end

-- Run just the next pending migration
function Runner:run_next(options)
    options = options or {}

    -- Find the next migration
    local next_migration, err = self:get_next_migration(options)
    if err then
        return {
            status = "complete",
            message = err,
            migrations_found = 0,
            migrations_applied = 0,
            migrations_skipped = 0,
            migrations_failed = 0
        }
    end

    -- Track results
    local results = {
        status = "running",
        migrations_found = 1,
        migrations_applied = 0,
        migrations_skipped = 0,
        migrations_failed = 0,
        migrations = {}
    }

    -- Start timing
    local start_time = time.now()

    -- Set up migration options
    local migration_options = {
        database_id = self.database_id,
        direction = "up"
    }

    -- Execute the migration
    local result = execute_migration(next_migration.id, migration_options)

    if result and result.status == "error" then
        results.migrations_failed = 1
        table.insert(results.migrations, {
            id = next_migration.id,
            status = "error",
            error = result.error,
            description = next_migration.meta and next_migration.meta.description or ""
        })
        results.status = "error"
        results.error = result.error
    elseif result and result.status == "applied" then
        results.migrations_applied = 1
        table.insert(results.migrations, {
            id = next_migration.id,
            status = "applied",
            description = next_migration.meta and next_migration.meta.description or "",
            duration = result.duration
        })
    else
        results.migrations_skipped = 1
        table.insert(results.migrations, {
            id = next_migration.id,
            status = "skipped",
            reason = result and result.reason or "Unknown",
            description = next_migration.meta and next_migration.meta.description or ""
        })
    end

    local end_time = time.now()
    results.duration = end_time:sub(start_time):milliseconds() / 1000 -- Convert to seconds

    -- Determine final status
    if results.status ~= "error" then
        results.status = "complete"
    end

    return results
end

-- Roll back migrations
function Runner:rollback(options)
    options = options or {}

    -- Get database connection
    local db, err = sql.get(self.database_id)
    if err then
        return {
            status = "error",
            error = "Failed to connect to database: " .. err
        }
    end

    -- Initialize tracking table
    local _, init_err = repository.init_tracking_table(db)
    if init_err then
        db:release()
        return {
            status = "error",
            error = "Failed to initialize migration tracking table: " .. init_err
        }
    end

    -- Get applied migrations sorted by most recent first
    local applied_migrations, err = repository.get_migrations(db)
    if err then
        db:release()
        return {
            status = "error",
            error = "Failed to get applied migrations: " .. err
        }
    end

    db:release()

    if not applied_migrations or #applied_migrations == 0 then
        return {
            status = "complete",
            message = "No migrations to roll back",
            migrations_found = 0,
            migrations_reverted = 0,
            migrations_skipped = 0,
            migrations_failed = 0
        }
    end

    -- Sort by applied_at descending (most recent first)
    table.sort(applied_migrations, function(a, b)
        return a.applied_at > b.applied_at
    end)

    -- Determine how many migrations to roll back
    local count = options.count or 1 -- Default to rolling back just the last migration
    if count > #applied_migrations then
        count = #applied_migrations
    end

    local to_rollback = {}
    for i = 1, count do
        table.insert(to_rollback, applied_migrations[i])
    end

    -- Track results
    local results = {
        status = "running",
        migrations_found = #to_rollback,
        migrations_reverted = 0,
        migrations_skipped = 0,
        migrations_failed = 0,
        migrations = {}
    }

    -- Start timing
    local start_time = time.now()

    -- Execute rollbacks
    for _, migration in ipairs(to_rollback) do
        -- Set up migration options
        local migration_options = {
            database_id = self.database_id,
            direction = "down",
            id = migration.id
        }

        -- Execute the migration
        local result = execute_migration(migration.id, migration_options)

        if result and result.status == "error" then
            results.migrations_failed = results.migrations_failed + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "error",
                error = result.error,
                description = migration.description or ""
            })

            -- Stop on first error
            results.status = "error"
            results.error = result.error
            break
        elseif result and result.status == "reverted" then
            results.migrations_reverted = results.migrations_reverted + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "reverted",
                description = migration.description or "",
                duration = result.duration
            })
        else
            results.migrations_skipped = results.migrations_skipped + 1
            table.insert(results.migrations, {
                id = migration.id,
                status = "skipped",
                reason = result and result.reason or "Unknown",
                description = migration.description or ""
            })
        end
    end

    local end_time = time.now()
    results.duration = end_time:sub(start_time):milliseconds() / 1000 -- Convert to seconds

    -- Determine final status
    if results.status ~= "error" then
        results.status = "complete"
    end

    return results
end

-- Get migration status for this database
function Runner:status(options)
    options = options or {}

    -- Find available migrations
    local migrations, find_err = self:find_migrations(options)
    if find_err then
        return {
            status = "error",
            error = find_err
        }
    end

    -- Count metrics
    local status_report = {
        database_id = self.database_id,
        db_type = nil, -- Will be filled in below
        total_migrations = #migrations,
        applied_migrations = 0,
        pending_migrations = 0,
        migrations = {}
    }

    -- Get database type
    local db, err = sql.get(self.database_id)
    if not err then
        local db_type, _ = db:type()
        status_report.db_type = db_type
        db:release()
    end

    -- Process migrations
    for _, migration in ipairs(migrations) do
        local migration_status = {
            id = migration.id,
            description = migration.meta and migration.meta.description or "",
            timestamp = migration.meta and migration.meta.timestamp or "",
            tags = migration.meta and migration.meta.tags or {},
            status = migration.applied and "applied" or "pending",
            applied_at = migration.applied_at
        }

        if migration.applied then
            status_report.applied_migrations = status_report.applied_migrations + 1
        else
            status_report.pending_migrations = status_report.pending_migrations + 1
        end

        table.insert(status_report.migrations, migration_status)
    end

    return status_report
end

return runner
