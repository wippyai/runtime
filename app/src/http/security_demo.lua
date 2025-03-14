local http = require("http")
local security = require("security")
local json = require("json")
local time = require("time")

-- Demo handler for security functionality
local function handler()
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Set up JSON response
    res:set_content_type(http.CONTENT.JSON)

    -- Get request action
    local action = req:query("action") or "demo"

    -- Execute different actions based on the query parameter
    if action == "create_actor" then
        return handle_create_actor(req, res)
    elseif action == "create_token" then
        return handle_create_token(req, res)
    elseif action == "validate_token" then
        return handle_validate_token(req, res)
    elseif action == "check_permission" then
        return handle_check_permission(req, res)
    else
        return handle_demo_info(req, res)
    end
end

-- Handle creating an actor
function handle_create_actor(req, res)
    -- Get actor parameters from request
    local id = req:query("id") or req:form("id")
    if not id or id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing actor ID",
            details = "Please provide an 'id' parameter"
        })
        return
    end

    -- Parse metadata from request if provided as JSON
    local metadata = {}
    local meta_json = req:query("meta") or req:form("meta")
    if meta_json and meta_json ~= "" then
        local meta_parsed, json_err = json.decode(meta_json)
        if not json_err and type(meta_parsed) == "table" then
            metadata = meta_parsed
        else
            -- Handle individual metadata fields
            local role = req:query("role") or req:form("role")
            if role and role ~= "" then
                metadata.role = role
            end

            local org = req:query("org") or req:form("org")
            if org and org ~= "" then
                metadata.org = org
            end
        end
    end

    -- Create the actor
    local actor = security.new_actor(id, metadata)

    -- Return the actor details
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
function handle_create_token(req, res)
    -- Get actor parameters from request
    local actor_id = req:query("actor_id") or req:form("actor_id")
    if not actor_id or actor_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing actor ID",
            details = "Please provide an 'actor_id' parameter"
        })
        return
    end

    -- Get scope name (if provided)
    local scope_name = req:query("scope") or req:form("scope") or "global:admin"
    local scope

    if scope_name and scope_name ~= "" then
        -- Get named scope from registry
        local named_scope, scope_err = security.named_scope(scope_name)

        if scope_err or not named_scope then
            res:set_status(http.STATUS.BAD_REQUEST)
            res:write_json({
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
                error = "Failed to get admin policy",
                details = policy_err or "Policy not found"
            })
            return
        end

        scope = security.new_scope({policy})
    end

    -- Parse actor metadata
    local metadata = {}
    local meta_json = req:query("meta") or req:form("meta")
    if meta_json and meta_json ~= "" then
        local meta_parsed, json_err = json.decode(meta_json)
        if not json_err and type(meta_parsed) == "table" then
            metadata = meta_parsed
        end
    end

    -- Create actor
    local actor = security.new_actor(actor_id, metadata)

    -- Get token expiration (default to 24 hours as per config)
    local expiration_str = req:query("expiration") or req:form("expiration") or "24h"

    -- Get token store from system.security:auth.tokens
    local token_store, ts_err = security.token_store("system.security:auth.tokens")
    if not token_store then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get token store",
            details = ts_err or "Unknown error"
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
function handle_validate_token(req, res)
    -- Get token from request
    local token = req:query("token") or req:form("token")
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
            error = "Missing token",
            details = "Please provide a 'token' parameter or Authorization header"
        })
        return
    end

    -- Get token store
    local token_store, ts_err = security.token_store("system.security:auth.tokens")
    if not token_store then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get token store",
            details = ts_err or "Unknown error"
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
            error = "Invalid token",
            details = validate_err or "Token validation failed"
        })
        return
    end

    -- Check optional permission
    local action_check = req:query("action_check")
    local resource = req:query("resource")
    local permission_result

    if action_check and resource then
        -- Evaluate permission using the scope from the token
        permission_result = scope:evaluate(actor, action_check, resource)
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
        },
        permission = permission_result and {
            action = action_check,
            resource = resource,
            result = permission_result
        } or nil
    })
end

-- Handle checking permissions
function handle_check_permission(req, res)
    -- Get parameters from request
    local action = req:query("action") or req:form("action")
    local resource = req:query("resource") or req:form("resource")

    if not action or action == "" or not resource or resource == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing parameters",
            details = "Please provide 'action' and 'resource' parameters"
        })
        return
    end

    -- Parse metadata for more complex permission checks
    local metadata = {}
    local meta_json = req:query("metadata") or req:form("metadata")
    if meta_json and meta_json ~= "" then
        local meta_parsed, json_err = json.decode(meta_json)
        if not json_err and type(meta_parsed) == "table" then
            metadata = meta_parsed
        end
    end

    -- Check for token-based authentication
    local token = req:header("Authorization")
    if token and token:find("Bearer ") == 1 then
        token = token:sub(8)

        -- Get token store
        local token_store, ts_err = security.token_store("system.security:auth.tokens")
        if not token_store then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                error = "Failed to get token store",
                details = ts_err or "Unknown error"
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
                error = "Invalid token",
                details = validate_err or "Token validation failed"
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

-- Handle demo info
function handle_demo_info(req, res)
    res:set_status(http.STATUS.OK)
    res:write_json({
        title = "Security API Demo",
        description = "This endpoint demonstrates various security features",
        available_actions = {
            {
                name = "create_actor",
                description = "Create a new actor",
                parameters = {
                    id = "Actor identifier (required)",
                    meta = "JSON string with metadata",
                    role = "Alternative to meta, sets role",
                    org = "Alternative to meta, sets organization"
                },
                url_example = "?action=create_actor&id=user123&role=admin&org=example"
            },
            {
                name = "create_token",
                description = "Create a token for an actor",
                parameters = {
                    actor_id = "Actor identifier (required)",
                    scope = "Named scope identifier (default: global:admin)",
                    meta = "JSON string with actor metadata",
                    expiration = "Token expiration (default: 24h)"
                },
                url_example = "?action=create_token&actor_id=user123&scope=global:admin&expiration=2h"
            },
            {
                name = "validate_token",
                description = "Validate a token and get actor/scope information",
                parameters = {
                    token = "Token string or Authorization header",
                    action_check = "Optional action to check",
                    resource = "Optional resource to check with action"
                },
                url_example = "?action=validate_token&token=eyJhbGci..."
            },
            {
                name = "check_permission",
                description = "Check if an action is allowed on a resource",
                parameters = {
                    action = "Action to check (required)",
                    resource = "Resource identifier (required)",
                    metadata = "JSON string with additional context",
                    ["Authorization Header"] = "Optional Bearer token for authentication"
                },
                url_example = "?action=check_permission&action=read&resource=document:123"
            }
        },
        auth_store_config = {
            name = "system.security:auth.tokens",
            token_length = 32,
            default_expiration = "24h"
        },
        policy_examples = {
            admin = "system:admin_policy",
            scope = "global:admin"
        },
        example_workflow = [=[
1. Create an actor:
   ?action=create_actor&id=alice&role=admin&org=acme

2. Create a token for the actor:
   ?action=create_token&actor_id=alice&expiration=1h

3. Validate the token (copy token from step 2):
   ?action=validate_token&token=[your-token]

4. Check permissions using the token:
   First set Authorization header to "Bearer [your-token]"
   Then call: ?action=check_permission&action=read&resource=document:123
]=]
    })
end

-- Export the function
return {
    handler = handler
}