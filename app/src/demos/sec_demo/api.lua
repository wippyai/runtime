local http = require("http")
local security = require("security")
local json = require("json")
local time = require("time")

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

    -- Get operation type
    local op = body.operation
    if not op then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing operation field",
            details =
            "Please specify an operation: create_actor, create_token, validate_token, revoke_token, or check_permission"
        })
        return
    end

    -- Route to the appropriate handler
    if op == "create_actor" then
        handle_create_actor(req, res, body)
    elseif op == "create_token" then
        handle_create_token(req, res, body)
    elseif op == "validate_token" then
        handle_validate_token(req, res, body)
    elseif op == "revoke_token" then
        handle_revoke_token(req, res, body)
    elseif op == "check_permission" then
        handle_check_permission(req, res, body)
    else
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid operation",
            details = "Supported operations: create_actor, create_token, validate_token, revoke_token, check_permission"
        })
    end
end

-- Create actor handler
function handle_create_actor(req, res, body)
    -- Validate request
    local id = body.id
    if not id or id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing actor ID"
        })
        return
    end

    -- Create actor with metadata
    local metadata = body.metadata or {}
    local actor = security.new_actor(id, metadata)

    -- Return response
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        }
    })
end

-- Create token handler
function handle_create_token(req, res, body)
    -- Validate request
    local actor_id = body.actor_id
    if not actor_id or actor_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing actor ID"
        })
        return
    end

    -- Create actor
    local metadata = body.actor_metadata or {}
    local actor = security.new_actor(actor_id, metadata)

    -- Get scope
    local scope_name = body.scope or "global:admin"
    local scope

    -- Get named scope or create admin scope
    if scope_name then
        local named_scope, err = security.named_scope(scope_name)
        if not named_scope then
            res:set_status(http.STATUS.BAD_REQUEST)
            res:write_json({
                success = false,
                error = "Invalid scope",
                details = err or "Scope not found"
            })
            return
        end
        scope = named_scope
    else
        local policy, err = security.policy("system:admin_policy")
        if not policy then
            res:set_status(http.STATUS.BAD_REQUEST)
            res:write_json({
                success = false,
                error = "Failed to get admin policy",
                details = err or "Policy not found"
            })
            return
        end
        scope = security.new_scope({ policy })
    end

    -- Get token expiration
    local expiration = body.expiration or "24h"

    -- Create token
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

    -- Create token
    local token, err = token_store:create(actor, scope, {
        expiration = expiration,
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

    -- Return token
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        token = token,
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        expiration = expiration
    })
end

-- Validate token handler
function handle_validate_token(req, res, body)
    -- Validate request
    local token = body.token
    if not token or token == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing token"
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

    print(token)

    -- Validate token
    local actor, scope, err = token_store:validate(token)

    -- Close token store
    token_store:close()

    if not actor or not scope then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Invalid token",
            details = err or "Token validation failed"
        })
        return
    end

    -- Get policy information
    local policies = scope:policies()
    local policy_ids = {}

    for i, policy in ipairs(policies) do
        table.insert(policy_ids, policy:id())
    end

    -- Return response
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        scope = {
            policy_count = #policies,
            policies = policy_ids
        }
    })
end

-- Revoke token handler
function handle_revoke_token(req, res, body)
    -- Validate request
    local token = body.token
    if not token or token == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing token"
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

    -- Revoke token
    local success, err = token_store:revoke(token)

    -- Close token store
    token_store:close()

    if not success then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to revoke token",
            details = err or "Unknown error"
        })
        return
    end

    -- Return response
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "Token revoked successfully"
    })
end

-- Check permission handler
function handle_check_permission(req, res, body)
    -- Validate request
    local token = body.token
    local action = body.action
    local resource = body.resource

    if not action or action == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing action parameter"
        })
        return
    end

    if not resource or resource == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing resource parameter"
        })
        return
    end

    -- Handle permission check by token
    if token and token ~= "" then
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

        -- Validate token
        local actor, scope, err = token_store:validate(token)

        -- Close token store
        token_store:close()

        if not actor or not scope then
            res:set_status(http.STATUS.UNAUTHORIZED)
            res:write_json({
                success = false,
                error = "Invalid token",
                details = err or "Token validation failed"
            })
            return
        end

        -- Check permission
        local metadata = body.metadata or {}
        local result = scope:evaluate(actor, action, resource, metadata)

        -- Return response
        res:set_status(http.STATUS.OK)
        res:write_json({
            success = true,
            actor = {
                id = actor:id(),
                metadata = actor:meta()
            },
            permission = {
                action = action,
                resource = resource,
                result = result,
                allowed = (result == "allow")
            }
        })
    else
        -- Use current security context
        local allowed = security.can(action, resource, body.metadata or {})

        -- Get current actor if available
        local actor = security.actor()
        local actor_info = nil

        if actor then
            actor_info = {
                id = actor:id(),
                metadata = actor:meta()
            }
        end

        -- Return response
        res:set_status(http.STATUS.OK)
        res:write_json({
            success = true,
            context_based = true,
            actor = actor_info,
            permission = {
                action = action,
                resource = resource,
                allowed = allowed
            }
        })
    end
end

return {
    handler = handler
}
