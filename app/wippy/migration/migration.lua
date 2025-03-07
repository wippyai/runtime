local migration = {}
local sql = require("sql")
local time = require("time")

local migration_core = require("migration_core")
local migration_db_proxy = require("migration_db_proxy")
local migration_schema = require("migration_schema")
local migration_reporter = require("migration_reporter")
local migration_executor = require("migration_executor")
local migration_registry = require("migration_registry")

-- Re-export event types
migration.event = migration_reporter.event

-- Run a migration function with full reporting
function migration.run(fn, options)
    -- Default options
    options = options or {}

    -- Create reporter based on options
    local reporter
    if options.reporter then
        -- Use provided reporter
        reporter = options.reporter
    elseif options.pid then
        -- Create reporter that targets a process
        reporter = migration_reporter.create({
            pid = options.pid,
            topic = options.topic or "migration:update",
            ref_id = options.ref_id
        })
    elseif options.target then
        -- Create reporter that targets a custom target
        reporter = migration_reporter.create({
            target = options.target,
            ref_id = options.ref_id
        })
    else
        -- Create no-op reporter
        reporter = migration_reporter.create_noop()
    end

    -- Run the migration function
    return migration_executor.run(fn, {
        database_id = options.database_id,
        db = options.db,
        reporter = reporter,
        dry_run = options.dry_run,
        transaction = options.transaction,
        force = options.force,
        db_namespace = options.db_namespace,
        record_migration = options.record_migration ~= false, -- Default to true
        ensure_schema = options.ensure_schema ~= false        -- Default to true
    })
end

-- Run a migration from registry by ID
function migration.run_by_id(id, options)
    options = options or {}

    -- Get migration from registry
    local entry, err = migration_registry.get(id)
    if err then
        return nil, "Failed to get migration: " .. err
    end

    if not entry or not entry.data or type(entry.data) ~= "function" then
        return nil, "Invalid migration function in registry"
    end

    -- Extract metadata
    local meta = entry.meta or {}

    -- Set namespace if not explicitly provided
    if not options.db_namespace and meta.db_namespace then
        options.db_namespace = meta.db_namespace
    end

    -- Set ref_id if not provided
    if not options.ref_id then
        options.ref_id = id
    end

    -- Run the migration function
    return migration.run(entry.data, options)
end

-- Run all migrations for a namespace
function migration.run_namespace(options)
    options = options or {}

    if not options.database_id then
        return nil, "Database ID is required"
    end

    if not options.db_namespace then
        return nil, "Database namespace is required"
    end

    -- Create reporter
    local reporter
    if options.reporter then
        reporter = options.reporter
    elseif options.pid then
        reporter = migration_reporter.create({
            pid = options.pid,
            topic = options.topic or "migration:update",
            ref_id = options.ref_id
        })
    elseif options.target then
        reporter = migration_reporter.create({
            target = options.target,
            ref_id = options.ref_id
        })
    else
        reporter = migration_reporter.create_noop()
    end

    -- Get DB connection
    local db, err = migration_db_proxy.get_connection(
        options.database_id,
        reporter
    )

    if err then
        reporter.report(migration_reporter.event.ERROR, {
            message = "Failed to connect to database: " .. err,
            timestamp = reporter.now()
        })

        return nil, "Failed to connect to database: " .. err
    end

    -- Initialize tracking table
    local _, init_err = migration_schema.init_tracking_table(db)
    if init_err then
        db:release()

        reporter.report(migration_reporter.event.ERROR, {
            message = "Failed to initialize migration tracking table: " .. init_err,
            timestamp = reporter.now()
        })

        return nil, "Failed to initialize migration tracking table: " .. init_err
    end

    -- Get database type
    local db_type, type_err = db:type()
    if type_err then
        db:release()

        reporter.report(migration_reporter.event.ERROR, {
            message = "Failed to determine database type: " .. type_err,
            timestamp = reporter.now()
        })

        return nil, "Failed to determine database type: " .. type_err
    end

    -- Find migrations in registry
    local find_options = {
        db_namespace = options.db_namespace,
        db_types = { db_type },
        tags = options.tags
    }

    local migrations, find_err = migration_registry.find(find_options)
    if find_err then
        db:release()

        reporter.report(migration_reporter.event.ERROR, {
            message = "Failed to find migrations: " .. find_err,
            timestamp = reporter.now()
        })

        return nil, "Failed to find migrations: " .. find_err
    end

    -- Release the initial DB connection; each migration will get its own
    db:release()

    if not migrations or #migrations == 0 then
        reporter.report(migration_reporter.event.COMPLETE, {
            status = "complete",
            message = "No migrations found for namespace: " .. options.db_namespace,
            total = 0,
            applied = 0,
            skipped = 0,
            failed = 0,
            timestamp = reporter.now()
        })

        return {
            status = "complete",
            message = "No migrations found for namespace: " .. options.db_namespace,
            migrations_found = 0,
            migrations_applied = 0,
            migrations_skipped = 0,
            migrations_failed = 0
        }
    end

    -- Report discovery
    reporter.report(migration_reporter.event.DISCOVER, {
        total = #migrations,
        namespace = options.db_namespace,
        db_type = db_type,
        timestamp = reporter.now()
    })

    -- Track results
    local results = {
        status = "running",
        namespace = options.db_namespace,
        db_type = db_type,
        migrations_found = #migrations,
        migrations_applied = 0,
        migrations_skipped = 0,
        migrations_failed = 0,
        migrations = {}
    }

    -- Run each migration
    for _, entry in ipairs(migrations) do
        -- Set migration options
        local migration_options = {
            database_id = options.database_id,
            db_namespace = options.db_namespace,
            dry_run = options.dry_run,
            force = options.force,
            transaction = options.transaction,
            reporter = reporter,
            ref_id = entry.id
        }

        -- Run the migration
        local result = migration.run_by_id(entry.id, migration_options)

        -- Track results
        table.insert(results.migrations, {
            id = entry.id,
            description = entry.meta and entry.meta.description or "",
            status = result.status,
            error = result.error,
            duration = result.duration
        })

        -- Update counters
        if result.status == "applied" then
            results.migrations_applied = results.migrations_applied + 1
        elseif result.status == "skipped" then
            results.migrations_skipped = results.migrations_skipped + 1
        elseif result.status == "error" then
            results.migrations_failed = results.migrations_failed + 1

            -- Stop on first error unless force flag is set
            if not options.force then
                results.status = "error"
                results.error = result.error
                break
            end
        end
    end

    -- Set final status
    if results.status ~= "error" then
        results.status = "complete"
    end

    -- Final completion report (although this might be redundant)
    reporter.report(migration_reporter.event.COMPLETE, {
        status = results.status,
        total = results.migrations_found,
        applied = results.migrations_applied,
        skipped = results.migrations_skipped,
        failed = results.migrations_failed,
        timestamp = reporter.now()
    })

    return results
end

-- Creating a migration definition function
function migration.define(fn)
    return function(options)
        return migration.run(fn, options)
    end
end

return migration
