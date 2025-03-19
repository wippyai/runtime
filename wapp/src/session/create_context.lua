local context_repo = require("context_repo")
local uuid = require("uuid")

local function create_context(args)
    -- todo: check actor!

    if not args or not args.user_id then
        return nil, "User ID is required"
    end

    -- Generate a context ID if not provided
    local context_id = args.context_id
    if not context_id or context_id == "" then
        local id, err = uuid.v4()
        if err then
            return nil, "Failed to generate context ID: " .. err
        end
        context_id = id
    end

    -- Set default type if not provided
    local context_type = args.type or "default"

    -- Handle data
    local data = args.data
    if not data then
        return nil, "Context data is required"
    end

    -- If data is a table, convert to JSON string
    if type(data) == "table" then
        local json = require("json")
        local encoded, err = json.encode(data)
        if err then
            return nil, "Failed to encode context data: " .. err
        end
        data = encoded
    end

    -- Create the context in the repository
    local result, err = context_repo.create(context_id, context_type, data)
    if err then
        return nil, err
    end

    return result
end

return { create_context = create_context }
