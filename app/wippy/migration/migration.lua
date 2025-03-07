local migration = {}
local sql = require("sql")
local time = require("time")

-- Internal state context
local _context = {
    current_migration = nil,
    current_database = nil,
    implementations = {},
    log = function(level, msg) end -- Default no-op logger
}

-- Define a specific migration
function migration.migration(description, fn)
    local old_migration = _context.current_migration
    _context.current_migration = {
        description = description,
        database_implementations = {}
    }

    -- Run the migration definition
    fn()

    -- Store migration
    table.insert(_context.implementations, _context.current_migration)
    _context.current_migration = old_migration

    return _context.current_migration
end

-- Define database-specific implementation
function migration.database(db_type, fn)
    if not _context.current_migration then
        error("database must be called within a migration block")
    end

    local old_database = _context.current_database
    _context.current_database = {
        type = db_type,
        check_needed = nil,
        up = nil,
        after = nil,
        down = nil
    }

    -- Execute database implementation definition
    fn()

    -- Store implementation
    _context.current_migration.database_implementations[db_type] = _context.current_database
    _context.current_database = old_database

    return migration
end

-- Define check if migration is needed
function migration.check_if_needed(fn)
    if not _context.current_database then
        error("check_if_needed must be called within a database block")
    end

    _context.current_database.check_needed = fn
    return migration
end

-- Define primary migration function
function migration.up(fn)
    if not _context.current_database then
        error("up must be called within a database block")
    end

    _context.current_database.up = fn
    return migration
end

-- Define post-migration steps
function migration.after(fn)
    if not _context.current_database then
        error("after must be called within a database block")
    end

    _context.current_database.after = fn
    return migration
end

-- Define rollback function
function migration.down(fn)
    if not _context.current_database then
        error("down must be called within a database block")
    end

    _context.current_database.down = fn
    return migration
end

-- Apply migration function with options
function migration.apply(options)
    options = options or {}

    -- Only proceed with execution if we have a database
    if not options.db then
        return {
            status = "skipped",
            reason = "No database connection provided",
            dry_run = options.dry_run
        }
    end

    -- Get database type
    local db_type, err = options.db:type()
    if err then
        return {
            status = "error",
            error = "Failed to determine database type: " .. err,
            dry_run = options.dry_run
        }
    end

    -- Set up result structure
    local result = {
        status = "pending",
        db_type = db_type,
        implementations = {},
        applied = false,
        dry_run = options.dry_run
    }

    -- Find matching migrations for this database type
    local matching_implementations = {}

    for _, m in ipairs(_context.implementations) do
        if m.database_implementations[db_type] then
            table.insert(matching_implementations, {
                description = m.description,
                implementation = m.database_implementations[db_type]
            })

            -- Add to result details
            result.implementations[#result.implementations + 1] = {
                description = m.description
            }

            -- Set description if not set
            if not result.description then
                result.description = m.description
            end
        end
    end

    -- Check if we have any matching implementations
    if #matching_implementations == 0 then
        result.status = "skipped"
        result.reason = "No implementations found for database type: " .. db_type
        return result
    end

    -- Exit early if this is a dry run
    if options.dry_run then
        result.status = "preview"
        return result
    end

    -- Start transaction for atomic migration application if requested
    local tx = nil
    if options.transaction then
        local tx_err
        tx, tx_err = options.db:begin()
        if tx_err then
            result.status = "error"
            result.error = "Failed to start transaction: " .. tx_err
            return result
        end
    end

    -- Use the database connection or transaction
    local db_conn = tx or options.db

    -- Track if any migration was applied
    local any_applied = false

    -- Run each migration implementation
    for i, m in ipairs(matching_implementations) do
        local impl_result = {
            description = m.description,
            status = "pending"
        }

        -- Run pre-check if available
        local needed = true
        local skip_reason = nil

        if m.implementation.check_needed then
            needed, skip_reason = m.implementation.check_needed(db_conn)
        end

        if not needed then
            impl_result.status = "skipped"
            impl_result.reason = skip_reason
        else
            -- Apply migration
            local start_time = time.now()
            local success, up_err = m.implementation.up(db_conn)

            if not success then
                impl_result.status = "error"
                impl_result.error = up_err

                -- Rollback transaction if we have one
                if tx then
                    tx:rollback()
                end

                result.status = "error"
                result.error = "Migration failed: " .. up_err
                return result
            end

            -- Apply after hooks if available
            if m.implementation.after then
                local after_success, after_err = m.implementation.after(db_conn)
                if not after_success then
                    impl_result.status = "error"
                    impl_result.error = "After hook failed: " .. after_err

                    -- Rollback transaction if we have one
                    if tx then
                        tx:rollback()
                    end

                    result.status = "error"
                    result.error = "After hook failed: " .. after_err
                    return result
                end
            end

            local end_time = time.now()
            impl_result.duration = end_time:sub(start_time):milliseconds() / 1000
            impl_result.status = "applied"
            any_applied = true
        end

        -- Store implementation result
        result.implementations[i].status = impl_result.status
        result.implementations[i].duration = impl_result.duration
        result.implementations[i].reason = impl_result.reason
        result.implementations[i].error = impl_result.error
    end

    -- Commit transaction if used
    if tx then
        local commit_success, commit_err = tx:commit()
        if not commit_success then
            result.status = "error"
            result.error = "Failed to commit transaction: " .. commit_err
            return result
        end
    end

    -- Set final status
    if any_applied then
        result.status = "applied"
        result.applied = true
    else
        result.status = "skipped"
        result.reason = "No migrations needed to be applied"
    end

    return result
end

-- Define migration
function migration.define(fn)
    -- Reset state
    _context.implementations = {}

    -- Set up global functions
    _G.migration = migration.migration
    _G.database = migration.database
    _G.check_if_needed = migration.check_if_needed
    _G.up = migration.up
    _G.after = migration.after
    _G.down = migration.down

    -- Define migration
    fn()

    -- Clean up globals
    _G.migration = nil
    _G.database = nil
    _G.check_if_needed = nil
    _G.up = nil
    _G.after = nil
    _G.down = nil

    -- Return apply function
    return migration.apply
end

return migration
