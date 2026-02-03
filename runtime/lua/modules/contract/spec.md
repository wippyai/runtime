# contract

Contract-based interface invocation with async support. Workflow, nondeterministic.

## Loading

```lua
local contract = require("contract")
```

## Dependencies

### Future (from funcs module)

Returned by async method calls (`instance:method_async()`).

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| response | () | Channel | Returns channel for receiving result |
| is_complete | () | boolean | Non-blocking check if complete |
| is_canceled | () | boolean | Returns true if canceled |
| result | () | Payload/table, boolean | Cached result if complete, nil + false otherwise |
| error | () | boolean, error | true + error if failed, false + nil otherwise |
| cancel | () | boolean, error | Cancels async operation |

### Payload (globally available)

Method results are wrapped as Payload objects. See `runtime/lua/modules/payload/spec.md`.

## Functions

### get(contract_id: string) → Contract, error

Retrieves a contract definition for introspection.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| contract_id | string | yes | - | Contract registry ID (e.g., "namespace:name") |

**Returns:**
- Success: Contract wrapper with introspection methods
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| Contract registry not found | errors.INTERNAL | no |
| Contract not found | errors.NOT_FOUND | no |

**Yields:** until contract retrieved from registry

### open(binding_id: string, scope?: table) → Instance, error

Opens a binding directly by ID, optionally with scope context.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| binding_id | string | yes | - | Binding ID, supports query params: "ns:binding?key=value" |
| scope | table | no | nil | Context values passed to implementation |

**Query parameters in binding_id:**

- Format: `"binding:id?key1=value1&key2=value2"`
- Values auto-converted: "true"/"false" → boolean, numbers → integer/float, else string
- Query params have lower priority than scope table

**Returns:**
- Success: Instance object with dynamic method access
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Invalid binding ID format | errors.INVALID | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| Binding not found | errors.NOT_FOUND | no |
| Open failed | errors.INTERNAL | no |

**Yields:** until instance opened

### find_implementations(contract_id: string) → string[], error

Lists all binding IDs that implement a contract.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| contract_id | string | yes | - | Contract registry ID |

**Returns:**
- Success: Array of binding ID strings
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| Contract registry not found | errors.INTERNAL | no |
| Get implementations failed | varies | varies |

### is(instance: Instance, contract_id: string) → boolean

Checks if an instance implements a specific contract.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| instance | Instance | yes | - | Instance userdata from open() |
| contract_id | string | yes | - | Contract ID to check |

**Returns:** boolean (true if implements, false otherwise)

## Types

### Contract

Returned by `contract.get()`. Provides contract introspection.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| id | () | string | Contract registry ID |
| methods | () | table[] | Array of method definitions |
| method | (name: string) | table, error | Single method definition or nil + error |
| implementations | () | string[], error | Binding IDs implementing this contract |
| open | (binding_id?: string, scope?: table) | Instance, error | Opens binding, uses default if binding_id nil |
| with_context | (ctx: table) | Contract | Returns new wrapper with merged context values |
| with_actor | (actor: Actor) | Contract | Returns new wrapper with security actor |
| with_scope | (scope: Scope) | Contract | Returns new wrapper with security scope |

#### contract:methods() → table[]

Returns array of method definitions.

**Method definition structure:**

| Field | Type | Notes |
|-------|------|-------|
| name | string | Method name |
| description | string | Method description |
| input_schemas | table[] | Optional, array of schema definitions |
| output_schemas | table[] | Optional, array of schema definitions |

**Schema definition structure:**

| Field | Type | Notes |
|-------|------|-------|
| format | string | Schema format (e.g., "json-schema") |
| definition | any | Schema definition (format-specific) |

#### contract:method(name: string) → table, error

Returns single method definition or error if not found.

**Returns:**
- Success: table with same structure as methods() entry
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Method not found | errors.NOT_FOUND | no |

#### contract:implementations() → string[], error

Returns array of binding IDs implementing this contract.

**Returns:**
- Success: Array of binding ID strings
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Get implementations failed | varies | varies |

#### contract:open(binding_id?: string, scope?: table) → Instance, error

Opens a binding for this contract.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| binding_id | string | no | nil | Specific binding, or nil for default |
| scope | table | no | nil | Context values, can be 2nd or 3rd arg |

**Parameter positions:**
- `contract:open()` - Uses default binding, no scope
- `contract:open(binding_id)` - Specific binding, no scope
- `contract:open(nil, scope)` - Default binding with scope
- `contract:open(binding_id, scope)` - Specific binding with scope

**Scope priority (highest to lowest):**
1. Explicit scope table parameter
2. wrapper.values (from with_context)
3. Query parameters in binding_id

**Returns:**
- Success: Instance object
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| No default binding | errors.NOT_FOUND | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| Open failed | errors.INTERNAL | no |

**Yields:** until instance opened

#### contract:with_context(ctx: table) → Contract

Creates new Contract wrapper with merged context values.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ctx | table | yes | - | Context key-value pairs |

**Returns:** New Contract wrapper with merged context

Context values are passed to bindings opened via this wrapper. Values from ctx override existing wrapper context.

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |

#### contract:with_actor(actor: Actor) → Contract

Creates new Contract wrapper with custom security actor.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| actor | Actor | yes | - | Security actor userdata |

**Returns:** New Contract wrapper with actor

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| Actor is nil | - | - |

#### contract:with_scope(scope: Scope) → Contract

Creates new Contract wrapper with custom security scope.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| scope | Scope | yes | - | Security scope userdata |

**Returns:** New Contract wrapper with scope

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| Scope is nil | - | - |

### Instance

Returned by `contract.open()` or `Contract:open()`. Provides dynamic method access.

Instance methods are accessed dynamically via `__index` metamethod:
- Sync: `instance:method_name(args...)` → result, error
- Async: `instance:method_name_async(args...)` → Future, error

Method names are validated against implemented contracts. Non-existent methods return nil.

**Dynamic method call:**

```lua
local result, err = instance:method_name(arg1, arg2, ...)
```

**Returns:**
- Success: Method result (auto-converted from Payload), nil
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| Call failed | varies | varies |
| Result conversion failed | errors.INTERNAL | no |

**Yields:** until method completes

**Dynamic async method call:**

```lua
local future, err = instance:method_name_async(arg1, arg2, ...)
```

**Returns:**
- Success: Future object, nil
- Error: nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Permission denied | errors.PERMISSION_DENIED | no |
| No process context | errors.INTERNAL | no |
| Subscribe failed | errors.INTERNAL | no |
| Async call failed | errors.INTERNAL | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local instance, err = contract.open("ns:binding")
if err then
    if err:kind() == errors.PERMISSION_DENIED then
        -- access denied
    elseif err:kind() == errors.NOT_FOUND then
        -- contract/binding not found
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.PERMISSION_DENIED`, `errors.NOT_FOUND`, `errors.INTERNAL`

## Example

```lua
local contract = require("contract")

-- Get contract definition for introspection
local greeter, err = contract.get("app.service:greeter")
if err then error(err) end

-- Inspect contract methods
local methods = greeter:methods()
for _, m in ipairs(methods) do
    print(m.name, m.description)
end

-- Open binding directly
local instance, err = contract.open("app.service:greeter_impl", {
    user_id = "12345",
    tenant = "acme"
})
if err then error(err) end

-- Call sync method
local greeting, err = instance:greet("Alice")
if err then error(err) end
print(greeting)

-- Call async method
local future, err = instance:process_async(data)
if err then error(err) end

-- Receive async result
local result = future:response():receive()
print(result:data())

-- Open via contract wrapper
local calc, err = greeter:open("app.service:calc_impl")
if err then error(err) end

local sum, err = calc:add(10, 32)
if err then error(err) end
print(sum)

-- Check what contract an instance implements
if contract.is(instance, "app.service:greeter") then
    print("Instance is a greeter")
end

-- Find all implementations
local impls, err = contract.find_implementations("app.service:greeter")
if err then error(err) end
for _, impl_id in ipairs(impls) do
    print("Implementation:", impl_id)
end

-- Open with query parameters
local svc, err = contract.open("app:service?debug=true&timeout=5000")
if err then error(err) end
```
