<!-- SPDX-License-Identifier: MPL-2.0 -->

# security

Security actors, scopes, and policies for access control. Security, nondeterministic.

## Loading

```lua
local security = require("security")
```

## Functions

### actor() → Actor | nil

Returns the current security actor from the execution context.

**Returns:**
- Success: `Actor` - current actor if one exists in context
- No actor: `nil` - no actor in current execution context

### scope() → Scope | nil

Returns the current security scope from the execution context.

**Returns:**
- Success: `Scope` - current scope if one exists in context
- No scope: `nil` - no scope in current execution context

### can(action: string, resource: string, meta?: table) → boolean

Checks if the current context allows the specified action on a resource.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| action | string | yes | - | Action to check (e.g., "read", "write") |
| resource | string | yes | - | Resource identifier |
| meta | table | no | nil | Additional metadata for policy evaluation |

**Returns:** `boolean` - true if allowed, false if denied or no security context

**Example:**

```lua
if security.can("read", "user:123") then
    -- perform read operation
end
```

### policy(id: string) → Policy, error

Retrieves a policy from the registry by its ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Policy ID in format "namespace:name" |

**Returns:**
- Success: `Policy, nil` - policy object and no error
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| permission denied | errors.INVALID | no |
| policy not found | errors.INTERNAL | no |

### named_scope(id: string) → Scope, error

Retrieves a policy group (named scope) from the registry by its ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Policy group ID in format "namespace:name" |

**Returns:**
- Success: `Scope, nil` - scope containing all policies in the group
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| permission denied | errors.INVALID | no |
| policy group not found | errors.INTERNAL | no |

### new_scope(policies?: Policy[]) → Scope

Creates a new custom scope, optionally initialized with policies.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| policies | Policy[] | no | nil | Array of policy objects to include |

**Returns:** `Scope` - new scope object

**Raises:** Lua error if permission denied to create custom scopes

**Example:**

```lua
local scope = security.new_scope()

-- or with policies
local pol, _ = security.policy("app:allow-read")
local scope = security.new_scope({ pol })
```

### new_actor(id: string, meta?: table) → Actor

Creates a new actor with the specified ID and metadata.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Unique actor identifier |
| meta | table | no | nil | Metadata key-value pairs |

**Returns:** `Actor` - new actor object

**Raises:** Lua error if permission denied to create actor with specified ID

**Example:**

```lua
local actor = security.new_actor("user123", {
    role = "admin",
    department = "engineering"
})
```

### token_store(id: string) → TokenStore, error

Acquires a token store resource for managing authentication tokens.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Token store resource ID in format "namespace:name" |

**Returns:**
- Success: `TokenStore, nil` - token store object and no error
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| id required | errors.INVALID | no |
| permission denied | errors.INVALID | no |
| resource registry not found | errors.INTERNAL | no |
| acquire failed | errors.INTERNAL | no |
| not a token store | errors.INTERNAL | no |

## Types

### Actor

Returned by `security.actor()` and `security.new_actor()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| id | () | string | Actor's unique identifier |
| meta | () | table | Actor's metadata as key-value table |

### Policy

Returned by `security.policy()`. Represents an authorization policy.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| id | () | string | Policy ID in format "namespace:name" |
| evaluate | (actor: Actor, action: string, resource: string, meta?: table) | string | Returns "allow", "deny", or "undefined" |

#### policy:evaluate(actor: Actor, action: string, resource: string, meta?: table) → string

Evaluates the policy for the given actor, action, and resource.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| actor | Actor | yes | - | Actor to evaluate |
| action | string | yes | - | Action being performed |
| resource | string | yes | - | Resource being accessed |
| meta | table | no | nil | Additional evaluation metadata |

**Returns:** `string` - one of "allow", "deny", or "undefined"

### Scope

Returned by `security.scope()`, `security.named_scope()`, and `security.new_scope()`. An immutable collection of policies.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| with | (policy: Policy) | Scope | Returns new scope with policy added |
| without | (policyID: string \| Policy \| table) | Scope | Returns new scope without specified policy |
| evaluate | (actor: Actor, action: string, resource: string, meta?: table) | string | Evaluates all policies, returns "allow", "deny", or "undefined" |
| contains | (policyID: string \| Policy \| table) | boolean | Checks if policy is in scope |
| policies | () | Policy[] | Returns array of all policies |

#### scope:with(policy: Policy) → Scope

Returns a new scope with the specified policy added.

**Raises:** Lua error if permission denied to add policy to scope

#### scope:without(policyID: string | Policy | table) → Scope

Returns a new scope without the specified policy.

| Param Type | Format | Example |
|------------|--------|---------|
| string | "namespace:name" | "app:read-only" |
| Policy | Policy object | policy from `security.policy()` |
| table | `{ns="namespace", name="name"}` | `{ns="app", name="read-only"}` |

**Raises:** Lua error if permission denied to remove policy from scope

#### scope:evaluate(actor: Actor, action: string, resource: string, meta?: table) → string

Evaluates all policies in the scope and returns the combined result.

**Returns:** `string` - one of "allow", "deny", or "undefined"

#### scope:contains(policyID: string | Policy | table) → boolean

Checks if the scope contains the specified policy.

| Param Type | Format | Example |
|------------|--------|---------|
| string | "namespace:name" | "app:read-only" |
| Policy | Policy object | policy from `security.policy()` |
| table | `{ns="namespace", name="name"}` | `{ns="app", name="read-only"}` |

**Returns:** `boolean` - true if policy is in scope, false otherwise

#### scope:policies() → Policy[]

Returns an array of all policies in the scope.

**Returns:** `Policy[]` - array of policy objects (1-indexed)

### TokenStore

Returned by `security.token_store()`. Manages authentication tokens.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| validate | (token: string) | Actor, Scope, error | Validates token, yields |
| create | (actor: Actor, scope: Scope, options?: table) | string, error | Creates token, yields |
| revoke | (token: string) | boolean, error | Revokes token, yields |
| close | () | boolean | Releases token store resource |

#### store:validate(token: string) → Actor, Scope, error

Validates a token and returns the associated actor and scope.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| token | string | yes | - | Token to validate |

**Returns:**
- Success: `Actor, Scope, nil` - actor, scope, and no error
- Error: `nil, nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| store closed | errors.INTERNAL | no |
| permission denied | errors.INVALID | no |
| validation failed | errors.INTERNAL | no |

**Yields:** until token validation completes

#### store:create(actor: Actor, scope: Scope, options?: table) → string, error

Creates a new authentication token for the actor with the specified scope.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| actor | Actor | yes | - | Actor to associate with token |
| scope | Scope | yes | - | Scope to associate with token |
| options | table | no | nil | Token creation options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| expiration | integer\|string | 0 | Token expiration: milliseconds or Go duration ("1h", "5m") |
| meta | table | nil | Additional metadata to store with token |

**Returns:**
- Success: `string, nil` - token string and no error
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| store closed | errors.INTERNAL | no |
| permission denied | errors.INVALID | no |
| invalid expiration | errors.INVALID | no |
| creation failed | errors.INTERNAL | no |

**Yields:** until token creation completes

**Example:**

```lua
local token, err = store:create(actor, scope, {
    expiration = "1h",
    meta = { login_time = os.time() }
})
```

#### store:revoke(token: string) → boolean, error

Revokes a token, making it invalid for future validation.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| token | string | yes | - | Token to revoke |

**Returns:**
- Success: `true, nil` - token revoked successfully
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| store closed | errors.INTERNAL | no |
| permission denied | errors.INVALID | no |
| revocation failed | errors.INTERNAL | no |

**Yields:** until token revocation completes

#### store:close() → boolean

Releases the token store resource. After calling close, the store cannot be used.

**Returns:** `boolean` - true

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local store, err = security.token_store("app:tokens")
if err then
    if err:kind() == errors.INVALID then
        -- permission denied or bad input
    elseif err:kind() == errors.INTERNAL then
        -- internal error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local security = require("security")

-- Get current security context
local actor = security.actor()
local scope = security.scope()

if actor then
    print("Current actor:", actor:id())
end

-- Check permissions
if security.can("read", "user:123") then
    -- perform read
end

-- Create custom actor and scope
local custom_actor = security.new_actor("service-worker", {
    type = "service"
})

local policy, err = security.policy("app:read-only")
if err then error(err) end

local custom_scope = security.new_scope():with(policy)

-- Evaluate policy
local result = custom_scope:evaluate(custom_actor, "read", "resource")
if result == "allow" then
    -- allowed
elseif result == "deny" then
    -- denied
else
    -- undefined
end

-- Token management
local store, err = security.token_store("app:tokens")
if err then error(err) end

local token, err = store:create(custom_actor, custom_scope, {
    expiration = "1h"
})
if err then error(err) end

-- Validate token later
local validated_actor, validated_scope, err = store:validate(token)
if err then error(err) end

print("Token actor:", validated_actor:id())

-- Clean up
store:close()
```
