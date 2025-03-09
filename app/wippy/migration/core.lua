local migration_core = {}

-- Create a new context for migration definitions
function migration_core.create_context()
    return {
        current_migration = nil,
        current_database = nil,
        implementations = {}
    }
end

-- Define a specific migration
function migration_core.create_migration_fn(context)
    return function(description, fn)
        local old_migration = context.current_migration
        context.current_migration = {
            description = description,
            database_implementations = {}
        }

        -- Run the migration definition
        fn()

        -- Store migration
        table.insert(context.implementations, context.current_migration)
        context.current_migration = old_migration

        return context.current_migration
    end
end

-- Define database-specific implementation
function migration_core.create_database_fn(context)
    return function(db_type, fn)
        if not context.current_migration then
            error("database must be called within a migration block")
        end

        local old_database = context.current_database
        context.current_database = {
            type = db_type,
            precondition = nil,
            up = nil,
            after = nil,
            down = nil
        }

        -- Execute database implementation definition
        fn()

        -- Store implementation
        context.current_migration.database_implementations[db_type] = context.current_database
        context.current_database = old_database
    end
end

-- Define precondition function
function migration_core.create_precondition_fn(context)
    return function(fn)
        if not context.current_database then
            error("precondition must be called within a database block")
        end

        context.current_database.precondition = fn
    end
end

-- Define primary migration function
function migration_core.create_up_fn(context)
    return function(fn)
        if not context.current_database then
            error("up must be called within a database block")
        end

        context.current_database.up = fn
    end
end

-- Define post-migration steps
function migration_core.create_after_fn(context)
    return function(fn)
        if not context.current_database then
            error("after must be called within a database block")
        end

        context.current_database.after = fn
    end
end

-- Define rollback function
function migration_core.create_down_fn(context)
    return function(fn)
        if not context.current_database then
            error("down must be called within a database block")
        end

        context.current_database.down = fn
    end
end

-- Setup global DSL functions with a context
function migration_core.setup_globals(context)
    _G.migration = migration_core.create_migration_fn(context)
    _G.database = migration_core.create_database_fn(context)
    _G.precondition = migration_core.create_precondition_fn(context)
    _G.up = migration_core.create_up_fn(context)
    _G.after = migration_core.create_after_fn(context)
    _G.down = migration_core.create_down_fn(context)
end

-- Cleanup global DSL functions
function migration_core.cleanup_globals()
    _G.migration = nil
    _G.database = nil
    _G.precondition = nil
    _G.up = nil
    _G.after = nil
    _G.down = nil
end

-- Define migration with a function
function migration_core.define(fn)
    local context = migration_core.create_context()

    -- Setup global functions with our context
    migration_core.setup_globals(context)

    -- Run definition function
    fn()

    -- Cleanup globals
    migration_core.cleanup_globals()

    -- Return collected migrations
    return context.implementations
end

return migration_core
