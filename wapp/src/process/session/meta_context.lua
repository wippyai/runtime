local json = require("json")

-- Error constants
local ERR = {
    CONTEXT_KEY_REQUIRED = "Context key is required",
    METADATA_REQUIRED = "Metadata items are required",
    TITLE_REQUIRED = "Title is required"
}

-- ContextManager class
local meta_context = {}
meta_context.__index = meta_context

-- Constructor
function meta_context.new(session_state, upstream)
    local self = setmetatable({}, meta_context)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream

    -- Public session metadata
    self.public_metadata = {}

    return self
end

-- Update public session metadata
function meta_context:update_public_metadata(items)
    if not items or type(items) ~= "table" then
        return false, ERR.METADATA_REQUIRED
    end

    -- Process each metadata item
    for _, item in ipairs(items) do
        if not item.id then
            return false, "Metadata item ID is required"
        end

        -- Update or add item
        local found = false
        for i, existing in ipairs(self.public_metadata) do
            if existing.id == item.id then
                -- Update existing item
                self.public_metadata[i] = item
                found = true
                break
            end
        end

        if not found then
            -- Add new item
            table.insert(self.public_metadata, item)
        end
    end

    -- Notify clients about metadata update
    if self.upstream then
        self.upstream:update_session({
            metadata = self.public_metadata
        })
    end

    -- Record metadata change in session state
    self:log_metadata_change(self.public_metadata)

    return true
end

-- Remove metadata items by ID
function meta_context:remove_public_metadata(ids)
    if not ids or type(ids) ~= "table" then
        return false, "Metadata IDs are required"
    end

    -- Create a map of IDs to remove for quick lookup
    local to_remove = {}
    for _, id in ipairs(ids) do
        to_remove[id] = true
    end

    -- Filter metadata to keep only items not in the remove list
    local updated = {}
    for _, item in ipairs(self.public_metadata) do
        if not to_remove[item.id] then
            table.insert(updated, item)
        end
    end

    -- Update the metadata
    self.public_metadata = updated

    -- Notify clients about metadata update
    if self.upstream then
        self.upstream:update_session({
            metadata = self.public_metadata
        })
    end

    -- Record metadata change in session state
    self:log_metadata_change(self.public_metadata)

    return true
end

-- Log metadata changes
function meta_context:log_metadata_change(metadata)
    -- Create system message for metadata change
    local metadata_msg = {
        source = "system",
        metadata_change = true
    }

    local message = "Session metadata updated"

    -- Add system message
    self.state:add_system_message(message, metadata_msg)
end

-- Write to context
function meta_context:write_context(key, data)
    if not key then
        return false, ERR.CONTEXT_KEY_REQUIRED
    end

    -- Get the primary context ID from state
    local context_id = self.state.primary_context_id
    if not context_id then
        return false, "Primary context ID not found"
    end

    -- Get the current context data
    local context, err = self.state:get_context(context_id)
    if err then
        return false, "Failed to get context: " .. err
    end

    -- Parse context data
    local context_data = {}
    if context and context.data then
        local success, parsed = pcall(json.decode, context.data)
        if success then
            context_data = parsed
        end
    end

    -- Update the context data
    context_data[key] = data

    -- Save updated context
    local success, err = self.state:update_context(context_id, json.encode(context_data))
    if not success then
        return false, "Failed to update context: " .. err
    end

    return true
end

-- Delete from context
function meta_context:delete_context(key)
    if not key then
        return false, ERR.CONTEXT_KEY_REQUIRED
    end

    -- Get the primary context ID from state
    local context_id = self.state.primary_context_id
    if not context_id then
        return false, "Primary context ID not found"
    end

    -- Get the current context data
    local context, err = self.state:get_context(context_id)
    if err then
        return false, "Failed to get context: " .. err
    end

    -- Parse context data
    local context_data = {}
    if context and context.data then
        local success, parsed = pcall(json.decode, context.data)
        if success then
            context_data = parsed
        end
    end

    -- Remove the key
    context_data[key] = nil

    -- Save updated context
    local success, err = self.state:update_context(context_id, json.encode(context_data))
    if not success then
        return false, "Failed to update context: " .. err
    end

    return true
end

-- Get public metadata
function meta_context:get_public_metadata()
    return self.public_metadata
end

-- Export constants
meta_context.ERR = ERR

return meta_context
