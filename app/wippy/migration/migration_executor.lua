local migration_executor = {}
local sql = require("sql")
local time = require("time")

local migration_core = require("migration_core")
local migration_db_proxy = require("migration_db_proxy")
local migration_schema = require("migration_schema")
local migration_reporter = require("migration_reporter")

-- Execute a single migration with proper transaction handling
function migration_executor.execute_migration(migration_item, options)
    options = options or {}

    local db = options.db
    local reporter = options.reporter or migration_reporter.create_noop()
    local dry_run = options.dry_run
    local use_transaction = options.transaction ~= false -- Default to true

    -- Report starting migration
    reporter.report(migration_reporter.event.START, {
        description = migration_item.description,
        db_type = options.db_type,
        timestamp = reporter.now()
    })

    -- Exit early if this is a dry run
    if dry_run then
        reporter.report(migration_reporter.event.SKIPPED, {
            description = migration_item.description,
            reason = "Dry run mode",
            timestamp = reporter.now()
        })

        return {
            status = "preview",
            description = migration_item.description,
            db_type = options.db_type
        }
    end

    -- Find the implementation for this database type
    local impl = migration_item.database_implementations[options.db_type]
    if not impl then
        reporter.report(migration_reporter.event.SKIPPED, {
            description = migration_item.description,
            reason = "No implementation for database type: " .. options.db_type,
            timestamp = reporter.now()
        })

        return {
            status = "skipped",
            description = migration_item.description,
            reason = "No implementation for database type: " .. options.db_type
        }
    end

    -- Check precondition if available
    local needed = true
    local skip_reason = nil

    if impl.precondition then
        needed, skip_reason = impl.precondition(db)
    end

    if not needed then
        reporter.report(migration_reporter.event.SKIPPED, {
            description = migration_item.description,
            reason = skip_reason,
            timestamp = reporter.now()
        })

        return {
            status = "skipped",
            description = migration_item.description,
            reason = skip_reason
        }
    end

    -- Start transaction for atomic migration application if requested
    local tx = nil
    if use_transaction then
        local tx_err
        tx, tx_err = db:begin()
        if tx_err then
            reporter.report(migration_reporter.event.ERROR, {
                description = migration_item.description,
                error = "Failed to start transaction: " .. tx_err,
                timestamp = reporter.now()
            })

            return {
                status = "error",
                description = migration_item.description,
                error = "Failed to start transaction: " .. tx_err
            }
        end
    end

    -- Use the transaction or direct connection
    local db_conn = tx or db

    -- Apply migration
    local start_time = reporter.now()
    local success, up_err = impl.up(db_conn)

    if not success then
        -- Rollback transaction if we have one
        if tx then tx:rollback() end

        reporter.report(migration_reporter.event.FAILED, {
            description = migration_item.description,
            error = up_err,
            timestamp = reporter.now()
        })

        return {
            status = "error",
            description = migration_item.description,
            error = up_err
        }
    end

    -- Record migration in tracking table - WITHIN the transaction
    local migration_id = options.id or migration_item.description:gsub("%s+", "_"):lower()
    local record_ok, record_err

    if options.record_migration then
        record_ok, record_err = migration_schema.record_migration(
            db_conn,
            migration_id,
            options.db_namespace or "",
            migration_item.description,
            "applied",
            0, -- Will update duration after commit
            { timestamp = start_time }
        )

        if not record_ok then
            -- Rollback transaction if we have one
            if tx then tx:rollback() end

            reporter.report(migration_reporter.event.FAILED, {
                description = migration_item.description,
                error = "Failed to record migration: " .. record_err,
                timestamp = reporter.now()
            })

            return {
                status = "error",
                description = migration_item.description,
                error = "Failed to record migration: " .. record_err
            }
        end
    end

    -- Apply after hooks if available
    if impl.after then
        local after_success, after_err = impl.after(db_conn)
        if not after_success then
            -- Rollback transaction if we have one
            if tx then tx:rollback() end

            reporter.report(migration_reporter.event.FAILED, {
                description = migration_item.description,
                error = "After hook failed: " .. after_err,
                timestamp = reporter.now()
            })

            return {
                status = "error",
                description = migration_item.description,
                error = "After hook failed: " .. after_err
            }
        end
    end

    -- Commit transaction if used
    if tx then
        local commit_success, commit_err = tx:commit()
        if not commit_success then
            reporter.report(migration_reporter.event.FAILED, {
                description = migration_item.description,
                error = "Failed to commit transaction: " .. commit_err,
                timestamp = reporter.now()
            })

            return {
                status = "error",
                description = migration_item.description,
                error = "Failed to commit transaction: " .. commit_err
            }
        end
    end

    local end_time = reporter.now()
    local duration = reporter.duration(start_time, end_time)

    -- If we're recording migrations, update the duration (outside transaction)
    if options.record_migration and not options.dry_run then
        -- This is best-effort after commit
        migration_schema.update_migration_duration(options.db, migration_id, duration)
    end

    reporter.report(migration_reporter.event.APPLIED, {
        description = migration_item.description,
        duration = duration,
        timestamp = end_time
    })

    return {
        status = "applied",
        description = migration_item.description,
        duration = duration
    }
end

-- Run a migration definition function and execute its migrations
function migration_executor.run(fn, options)
    options = options or {}

    local reporter = options.reporter or migration_reporter.create_noop()

    -- Define migrations using the core DSL
    local implementations = migration_core.define(fn)

    -- Get a database connection
    local db, err
    if options.db then
        -- Use provided DB connection
        db = options.db
    else
        if options.database_id then
            -- Get a new connection
            db, err = migration_db_proxy.get_connection(
                options.database_id,
                reporter
            )
            if err then
                reporter.report(migration_reporter.event.ERROR, {
                    message = "Failed to connect to database: " .. err,
                    timestamp = reporter.now()
                })

                return {
                    status = "error",
                    error = "Failed to connect to database: " .. err
                }
            end
        else
            reporter.report(migration_reporter.event.ERROR, {
                message = "No database connection or ID provided",
                timestamp = reporter.now()
            })

            return {
                status = "error",
                error = "No database connection or ID provided"
            }
        end
    end

    -- Ensure we have the migrations tracking table
    if options.ensure_schema then
        local schema_ok, schema_err = migration_schema.init_tracking_table(db)
        if not schema_ok then
            reporter.report(migration_reporter.event.ERROR, {
                message = "Failed to initialize migration tracking table: " .. schema_err,
                timestamp = reporter.now()
            })

            -- Release DB if we opened it
            if options.database_id then db:release() end

            return {
                status = "error",
                error = "Failed to initialize migration tracking table: " .. schema_err
            }
        end
    end

    -- Get database type
    local db_type, type_err = db:type()
    if type_err then
        reporter.report(migration_reporter.event.ERROR, {
            message = "Failed to determine database type: " .. type_err,
            timestamp = reporter.now()
        })

        -- Release DB if we opened it
        if options.database_id then db:release() end

        return {
            status = "error",
            error = "Failed to determine database type: " .. type_err
        }
    end

    -- Send plan with all migrations that will be applied
    local plan = {
        migrations = {},
        db_type = db_type,
        timestamp = reporter.now()
    }

    for _, m in ipairs(implementations) do
        -- Only include migrations that have an implementation for this DB type
        if m.database_implementations[db_type] then
            table.insert(plan.migrations, {
                description = m.description
            })
        end
    end

    reporter.report(migration_reporter.event.PLAN, plan)

    -- Apply each migration
    local results = {
        migrations = {},
        total = #implementations,
        applied = 0,
        skipped = 0,
        failed = 0,
        db_type = db_type
    }

    local start_time = reporter.now()

    for _, m in ipairs(implementations) do
        -- Only process migrations with an implementation for this DB type
        if m.database_implementations[db_type] then
            local result = migration_executor.execute_migration(m, {
                db = db,
                db_type = db_type,
                dry_run = options.dry_run,
                transaction = options.transaction,
                reporter = reporter,
                db_namespace = options.db_namespace,
                record_migration = options.record_migration
            })

            table.insert(results.migrations, result)

            if result.status == "applied" then
                results.applied = results.applied + 1
            elseif result.status == "skipped" then
                results.skipped = results.skipped + 1
            elseif result.status == "error" then
                results.failed = results.failed + 1

                -- Stop on first error unless force option is set
                if not options.force then
                    results.status = "error"
                    results.error = result.error
                    break
                end
            end
        else
            -- No implementation for this DB type
            results.skipped = results.skipped + 1
        end
    end

    local end_time = reporter.now()
    local duration = reporter.duration(start_time, end_time)

    -- Determine final status
    if not results.status then
        results.status = results.failed > 0 and "failed" or "complete"
    end

    results.duration = duration

    -- Report completion
    reporter.report(migration_reporter.event.COMPLETE, {
        status = results.status,
        total = results.total,
        applied = results.applied,
        skipped = results.skipped,
        failed = results.failed,
        duration = duration,
        timestamp = end_time
    })

    -- Release DB if we opened it
    if options.database_id then db:release() end

    return results
end

return migration_executor
