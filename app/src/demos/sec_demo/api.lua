local http = require("http")
local security = require("security")
local json = require("json")
local time = require("time")

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
    if err and req:content_type() == http.CONTENT.JSON and req:has_body() then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid JSON",
            details = err
        })
        return
    end

    body = body or {}
    local action = body.action or req:query("action")

    -- Handle different actions
    if action == "create_actor" then
        return handle_create_actor(req, res, body)
    elseif action == "create_token" then
        return handle_create_token(req, res, body)
    elseif action == "validate_token" then
        return handle_validate_token(req, res, body)
    elseif action == "revoke_token" then
        return handle_revoke_token(req, res, body)
    elseif action == "check_permission" then
        return handle_check_permission(req, res, body)
    else
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid action",
            actions = {
                "create_actor",
                "create_token",
                "validate_token",
                "revoke_token",
                "check_permission"
            }
        })
    end
end

-- Handle creating an actor
function handle_create_actor(req, res, body)
    -- Get actor ID
    local id = body.id
    if not id or id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing actor ID"
        })
        return
    end

    -- Parse metadata
    local metadata = body.metadata or {}

    -- Create the actor
    local actor = security.new_actor(id, metadata)

    -- Return success
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        }
    })
end

-- Handle creating a token
function handle_create_token(req, res, body)
    -- Get actor ID
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
    local metadata = body.metadata or {}
    local actor = security.new_actor(actor_id, metadata)

    -- Get or create scope
    local scope_name = body.scope or "global:admin"
    local scope

    if scope_name and scope_name ~= "" then
        -- Get named scope from registry
        local named_scope, scope_err = security.named_scope(scope_name)

        if scope_err or not named_scope then
            res:set_status(http.STATUS.BAD_REQUEST)
            res:write_json({
                success = false,
                error = "Invalid scope",
                details = scope_err or "Scope not found"
            })
            return
        end

        scope = named_scope
    else
        -- Create a basic scope with admin policy
        local policy, policy_err = security.policy("system:admin_policy")

        if policy_err or not policy then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                success = false,
                error = "Failed to get admin policy",
                details = policy_err or "Policy not found"
            })
            return
        end

        scope = security.new_scope({policy})
    end

    -- Get token expiration
    local expiration_str = body.expiration or "24h"

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

    -- Create token with metadata
    local token_meta = {
        ip = req:remote_addr() or "unknown",
        user_agent = req:header("User-Agent") or "unknown",
        created_at = time.now():format_rfc3339()
    }

    local token, token_err = token_store:create(actor, scope, {
        expiration = expiration_str,
        meta = token_meta
    })

    -- Close token store
    token_store:close()

    if not token then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to create token",
            details = token_err or "Unknown error"
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
        expiration = expiration_str
    })
end

-- Handle validating a token
function handle_validate_token(req, res, body)
    -- Get token
    local token = body.token

    if not token or token == "" then
        token = req:header("Authorization")

        -- Remove Bearer prefix if present
        if token and token:find("Bearer ") == 1 then
            token = token:sub(8)
        end
    end

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
            error = "Failed to get token store"
        })
        return
    end

    -- Validate token
    local actor, scope, validate_err = token_store:validate(token)

    -- Close token store
    token_store:close()

    if not actor or not scope then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Invalid token",
            details = validate_err or "Token validation failed"
        })
        return
    end

    -- Get all policies in the scope
    local policies = scope:policies()
    local policy_ids = {}

    for i, policy in ipairs(policies) do
        table.insert(policy_ids, policy:id())
    end

    -- Return validation result
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

-- Handle revoking a token
function handle_revoke_token(req, res, body)
    -- Get token
    local token = body.token

    if not token or token == "" then
        token = req:header("Authorization")

        -- Remove Bearer prefix if present
        if token and token:find("Bearer ") == 1 then
            token = token:sub(8)
        end
    end

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
            error = "Failed to get token store"
        })
        return
    end

    -- Revoke token
    local success, revoke_err = token_store:revoke(token)

    -- Close token store
    token_store:close()

    if not success then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to revoke token",
            details = revoke_err or "Unknown error"
        })
        return
    end

    -- Return success
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "Token successfully revoked"
    })
end

-- Handle checking permissions
function handle_check_permission(req, res, body)
    -- Get parameters
    local action = body.action
    local resource = body.resource

    if not action or action == "" or not resource or resource == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing parameters",
            details = "Please provide 'action' and 'resource' parameters"
        })
        return
    end

    -- Get metadata for permission check
    local metadata = body.metadata or {}

    -- Check for token-based authentication
    local token = body.token or req:header("Authorization")
    if token and token:find("Bearer ") == 1 then
        token = token:sub(8)
    end

    if token and token ~= "" then
        -- Get token store
        local token_store, err = security.token_store("system.security:auth.tokens")
        if not token_store then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                success = false,
                error = "Failed to get token store"
            })
            return
        end

        -- Validate token
        local actor, scope, validate_err = token_store:validate(token)

        -- Close token store
        token_store:close()

        if not actor or not scope then
            res:set_status(http.STATUS.UNAUTHORIZED)
            res:write_json({
                success = false,
                error = "Invalid token"
            })
            return
        end

        -- Check permission using the scope from the token
        local result = scope:evaluate(actor, action, resource, metadata)

        -- Return permission check result
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
        -- Use context actor and check permission
        local allowed = security.can(action, resource, metadata)

        -- Get current actor from context
        local actor = security.actor()
        local actor_info = nil

        if actor then
            actor_info = {
                id = actor:id(),
                metadata = actor:meta()
            }
        end

        -- Return permission check result
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