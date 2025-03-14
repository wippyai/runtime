```lua-- 1. Security package
local security = require("security")

-- Get current actor and scope from context
local actor = security.actor()
local scope = security.scope()

-- Check if action is allowed
local allowed = security.can("update", "document:123", {owner = "user456"})

-- 2. Scope and policy operations
local policy, err = security.policy("system:admin_policy")

-- Renamed from policy_group to named_scope
local scope, err = security.named_scope("global:admin")

local new_scope = security.new_scope({policy1, policy2})

-- Scope methods
local has_policy = scope:contains("system:admin_policy")
local policies_list = scope:policies()
local eval_result = scope:evaluate(actor, "delete", "resource:xyz", {owner = "user789"})
local extended_scope = scope:with(policy)
local reduced_scope = scope:without("system:read_only")

-- Apply policy to context temporarily
security.with_policy(policy)

-- 3. Token store operations
local token_store, err = security.token_store("system.security:auth.tokens")
local actor, scope, err = token_store:validate("eyJhbGciOiJ...")
local token, err = token_store:create(actor, scope, {
expiration = "24h",
meta = {
device = "mobile",
ip = "192.168.1.1"
}
})

token_store:close()
```

I apologize for the confusion. Let me provide the Go implementations for the security system components:

Here are the Go implementations for the Lua security module in your backend:

1. **security/module.go** - Main module that registers the functions and types
2. **security/actor.go** - Actor wrapper with ID and metadata access
3. **security/scope.go** - Scope implementation with policy management methods
4. **security/policy.go** - Policy wrapper to evaluate permissions
5. **security/token_store.go** - Token store with token validation and creation

The implementations provide a clean Lua API:

```lua
-- Get current context
local actor = security.actor()
local scope = security.scope()

-- Check permissions
local allowed = security.can("update", "document:123", {owner = "user456"})

-- Policy and scope management
local policy = security.policy("system:admin_policy")
local scope = security.named_scope("global:admin")
local new_scope = security.new_scope({policy1, policy2})
local has_policy = scope:contains("system:admin_policy")

-- Token operations
local token_store = security.token_store("system.security:auth.tokens")
local actor, scope, err = token_store:validate("token123")
local token, err = token_store:create(actor, scope, {expiration = "24h"})
token_store:close()
```

All resource-using operations properly handle resource lifecycle with appropriate release mechanisms.