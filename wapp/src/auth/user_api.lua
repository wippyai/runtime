local http = require("http")
local security = require("security")
local json = require("json")
local time = require("time")

-- Get the user repository
local user_repo = require("user_repo")

-- Main handler function
local function handler()
    local res = http.response()
    local req = http.request()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type
    res:set_content_type(http.CONTENT.JSON)

    -- Parse the request body as JSON
    local body, err = req:body_json()
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid JSON request",
            details = err
        })
        return
    end

    -- Get user ID
    local user_id = body.user_id
    if not user_id or user_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing user_id field"
        })
        return
    end

    -- Get metadata
    local metadata = body.metadata or {}

    -- Ensure sf_instance_token is included if provided
    if body.sf_instance_token then
        metadata.sf_instance_token = body.sf_instance_token
    end

    -- Create actor with metadata
    local actor = security.new_actor(user_id, metadata)

    -- Get named scope (using "global:user" as default)
    local scope_name = "global:user"
    local scope, err = security.named_scope(scope_name)

    if not scope then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to get user scope",
            details = err or "Scope not found"
        })
        return
    end

    -- Get token store
    local token_store, err = security.token_store("system.security:auth.tokens")
    if not token_store then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to get token store",
            details = err or "Unknown error"
        })
        return
    end

    -- Add metadata to token
    local token_meta = {
        ip = req:remote_addr() or "unknown",
        user_agent = req:header("User-Agent") or "unknown",
        created_at = time.now():format_rfc3339()
    }

    -- Create token with 24 hour expiration
    local token, err = token_store:create(actor, scope, {
        expiration = "24h",
        meta = token_meta
    })

    -- Close token store
    token_store:close()

    if not token then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to create token",
            details = err or "Unknown error"
        })
        return
    end

    -- Upsert user in database (only storing user_id and timestamps)
    local user, err = user_repo.upsert(user_id)

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to update user record",
            details = err
        })
        return
    end

    -- Return token and user info
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        token = token,
        user = user,
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        expiration = "24h"
    })
end

return {
    handler = handler
}
