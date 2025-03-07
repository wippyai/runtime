local sql = require("sql")
local time = require("time")
local migration_schema = require("migration_schema")
local migration_registry = require("migration_registry")

local migration_runner = {}

-- Run migrations
function migration_runner.run(options)
    options = options or {}

    if not options.database_id then
        return nil, "Database ID is required"
    end

    -- Get database connection
    local db, err = sql.get(options.database_id)
    if err then
        return nil, "Failed to connect to database: " .. err
    end

    -- Initialize tracking table
    local _, init_err = migration_schema.init_tracking_table(db)
    if init_err then
        db:release()
        return nil, "Failed to initialize migration tracking table: " .. init_err
    end

    -- Get database type
    local db_type, type_err = db:type()
    if type_err then
        db:release()
        return nil, "Failed to determine database type: " .. type_err
    end

    -- Find all migrations in registry
    local find_options = {
        db_namespace = options.db_namespace,
        db_types = { db_type },
        tags = options.tags
    }

    local migrations, find_err = migration_registry.find(find_options)
    if find_err then
        db:release()
        return nil, "Failed to find migrations: " .. find_err
    end

    if not migrations or #migrations == 0 then
        db:release()
        return {
            status = "complete",
            message = "No migrations found for specified criteria",
            migrations_found = 0,
            migrations_run = 0,
            migrations_applied = 0,
            migrations_skipped = 0,
            migrations_failed = 0,
            details = {}
        }
    end

    -- Get executed migrations
    local executed, exec_err = migration_schema.get_migrations(db, {
        db_namespace = options.db_namespace
    })

    if exec_err then
        db:release()
        return nil, "Failed to get executed migrations: " .. exec_err
    end

    -- Build map of executed migrations
    local executed_map = {}
    for _, m in ipairs(executed or {}) do
        executed_map[m.id] = m
    end

    -- Results container
    local results = {
        status = "running",
        migrations_found = #migrations,
        migrations_run = 0,
        migrations_applied = 0,
        migrations_skipped = 0,
        migrations_failed = 0,
        details = {}
    }

    -- Execute migrations
    for _, entry in ipairs(migrations) do
        -- Skip if already executed successfully (unless force flag is set)
        if executed_map[entry.id] and executed_map[entry.id].status == "applied" and not options.force then
            results.migrations_skipped = results.migrations_skipped + 1
            table.insert(results.details, {
                id = entry.id,
                status = "skipped",
                reason = "Already applied",
                applied_at = executed_map[entry.id].applied_at,
                description = entry.meta and entry.meta.description or ""
            })
            goto continue
        end

        -- Load migration function
        local migration_fn = entry.data
        if type(migration_fn) ~= "function" then
            results.migrations_failed = results.migrations_failed + 1
            table.insert(results.details, {
                id = entry.id,
                status = "error",
                error = "Invalid migration function",
                description = entry.meta and entry.meta.description or ""
            })
            goto continue
        end

        -- Execute the migration
        local start_time = time.now()
        local migration_result = migration_fn({
            db = db,
            dry_run = options.dry_run,
            transaction = true
        })
        local end_time = time.now()
        local duration = end_time:sub(start_time):milliseconds() / 1000

        results.migrations_run = results.migrations_run + 1

        -- Record result
        if migration_result.status == "applied" then
            results.migrations_applied = results.migrations_applied + 1

            -- Record in tracking table (if not dry run)
            if not options.dry_run then
                local record_err
                _, record_err = migration_schema.record_migration(
                    db,
                    entry.id,
                    options.db_namespace or (entry.meta and entry.meta.db_namespace) or "",
                    (entry.meta and entry.meta.description) or migration_result.description or "",
                    "applied",
                    duration,
                    migration_result
                )

                if record_err then
                    migration_result.record_error = record_err
                end
            end
        elseif migration_result.status == "error" then
            results.migrations_failed = results.migrations_failed + 1

            -- Record in tracking table (if not dry run)
            if not options.dry_run then
                migration_schema.record_migration(
                    db,
                    entry.id,
                    options.db_namespace or (entry.meta and entry.meta.db_namespace) or "",
                    (entry.meta and entry.meta.description) or migration_result.description or "",
                    "failed",
                    duration,
                    migration_result
                )
            end
        else
            results.migrations_skipped = results.migrations_skipped + 1
        end

        -- Add to details
        table.insert(results.details, {
            id = entry.id,
            status = migration_result.status,
            duration = duration,
            description = (entry.meta and entry.meta.description) or migration_result.description or "",
            error = migration_result.error,
            reason = migration_result.reason,
            dry_run = options.dry_run
        })

        -- Stop on first error if not force mode
        if migration_result.status == "error" and not options.force then
            results.status = "error"
            results.error = migration_result.error
            break
        end

        ::continue::
    end

    -- Release database
    db:release()

    -- Final status
    if results.status ~= "error" then
        results.status = "complete"
    end

    return results
end

-- Run migrations by namespace
function migration_runner.run_namespace(options)
    options = options or {}

    if not options.database_id then
        return nil, "Database ID is required"
    end

    if not options.db_namespace then
        return nil, "Database namespace is required"
    end

    return migration_runner.run(options)
end

-- Run all migrations for all namespaces
function migration_runner.run_all(options)
    options = options or {}

    if not options.database_id then
        return nil, "Database ID is required"
    end

    -- Get all namespaces
    local namespaces, err = migration_registry.get_namespaces()
    if err then
        return nil, "Failed to get namespaces: " .. err
    end

    if #namespaces == 0 then
        return {
            status = "complete",
            message = "No migration namespaces found",
            namespaces = {},
            total_applied = 0,
            total_skipped = 0,
            total_failed = 0
        }
    end

    -- Run migrations for each namespace
    local overall_results = {
        status = "complete",
        message = "Migrations completed",
        namespaces = {},
        total_applied = 0,
        total_skipped = 0,
        total_failed = 0
    }

    for _, namespace in ipairs(namespaces) do
        local ns_options = {
            database_id = options.database_id,
            db_namespace = namespace,
            dry_run = options.dry_run,
            force = options.force,
            tags = options.tags
        }

        local ns_result, ns_err = migration_runner.run(ns_options)

        if ns_err then
            overall_results.status = "error"
            overall_results.error = "Error in namespace " .. namespace .. ": " .. ns_err
            break
        end

        -- Aggregate results
        overall_results.total_applied = overall_results.total_applied + (ns_result.migrations_applied or 0)
        overall_results.total_skipped = overall_results.total_skipped + (ns_result.migrations_skipped or 0)
        overall_results.total_failed = overall_results.total_failed + (ns_result.migrations_failed or 0)

        -- Store namespace result
        overall_results.namespaces[namespace] = {
            status = ns_result.status,
            applied = ns_result.migrations_applied,
            skipped = ns_result.migrations_skipped,
            failed = ns_result.migrations_failed
        }

        -- Stop on first error namespace
        if ns_result.status == "error" and not options.force then
            overall_results.status = "error"
            overall_results.error = "Failed in namespace " .. namespace .. ": " .. (ns_result.error or "unknown error")
            break
        end
    end

    return overall_results
end

-- Get migration status
function migration_runner.status(options)
    options = options or {}

    if not options.database_id then
        return nil, "Database ID is required"
    end

    -- Get database connection
    local db, err = sql.get(options.database_id)
    if err then
        return nil, "Failed to connect to database: " .. err
    end

    -- Initialize tracking table (if it doesn't exist)
    local _, init_err = migration_schema.init_tracking_table(db)
    if init_err then
        db:release()
        return nil, "Failed to initialize migration tracking table: " .. init_err
    end

    -- Get database type
    local db_type, type_err = db:type()
    if type_err then
        db:release()
        return nil, "Failed to determine database type: " .. type_err
    end

    -- Find all migrations in registry
    local find_options = {
        db_namespace = options.db_namespace,
        db_types = { db_type },
        tags = options.tags
    }

    local migrations, find_err = migration_registry.find(find_options)
    if find_err then
        db:release()
        return nil, "Failed to find migrations: " .. find_err
    end

    -- Get executed migrations
    local executed, exec_err = migration_schema.get_migrations(db, {
        db_namespace = options.db_namespace
    })

    if exec_err then
        db:release()
        return nil, "Failed to get executed migrations: " .. exec_err
    end

    -- Build map of executed migrations
    local executed_map = {}
    for _, m in ipairs(executed or {}) do
        executed_map[m.id] = m
    end

    -- Build status report
    local status_report = {
        database_id = options.database_id,
        db_namespace = options.db_namespace,
        db_type = db_type,
        total_migrations = #migrations,
        applied_migrations = 0,
        pending_migrations = 0,
        failed_migrations = 0,
        migrations = {}
    }

    for _, entry in ipairs(migrations) do
        local migration_status = {
            id = entry.id,
            description = entry.meta and entry.meta.description or "",
            timestamp = entry.meta and entry.meta.timestamp or "",
            tags = entry.meta and entry.meta.tags or {},
            status = "pending"
        }

        if executed_map[entry.id] then
            migration_status.status = executed_map[entry.id].status
            migration_status.applied_at = executed_map[entry.id].applied_at
            migration_status.duration = executed_map[entry.id].duration

            if executed_map[entry.id].status == "applied" then
                status_report.applied_migrations = status_report.applied_migrations + 1
            elseif executed_map[entry.id].status == "failed" then
                status_report.failed_migrations = status_report.failed_migrations + 1
            end
        else
            status_report.pending_migrations = status_report.pending_migrations + 1
        end

        table.insert(status_report.migrations, migration_status)
    end

    -- Release database
    db:release()

    return status_report
end

return migration_runner
