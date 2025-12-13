# templates

Template rendering engine using Jet syntax. Deterministic.

## Loading

```lua
local templates = require("templates")
```

## Functions

### get(id: string) → Set, error

Acquires a template set resource by registry ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Resource registry ID (e.g., "app:my_templates") |

**Returns:** `Set` - Template set object, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| id is empty string | errors.INVALID | no |
| Permission denied | errors.PERMISSION_DENIED | no |
| Resource not found | errors.INTERNAL | no |
| Resource registry unavailable | errors.INTERNAL | no |
| Resource is not a template set | errors.INTERNAL | no |

**Notes:**
- Template sets are acquired from the resource registry
- Must call `:release()` when done to free the resource
- Security policy controls access to template resources

## Types

### Set

Template set returned by `templates.get()`. Represents a collection of templates with shared configuration.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| render | (name: string, data?: table) | string, error | Renders template with data |
| release | () | boolean | Releases resource, returns true |

#### set:render(name: string, data?: table) → string, error

Renders a template by name with provided data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Template name within the set |
| data | table | no | {} | Variables to pass to template |

**Returns:** `string` - Rendered template output, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Set already released | errors.INTERNAL | no |
| name is empty string | errors.INVALID | no |
| Template not found | errors.NOT_FOUND | no |
| Render error | errors.INTERNAL | no |

**Notes:**
- Data table is converted to template variables (supports nested tables, arrays)
- Template globals configured in the set are available during render
- Jet template syntax supports control flow, iteration, inheritance
- Empty data table `{}` can be omitted

#### set:release() → boolean

Releases the template set resource.

**Returns:** `true` on success

**Notes:**
- Must be called when done with the template set
- Subsequent render calls will fail after release
- Safe to call multiple times (idempotent)

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local set, err = templates.get("app:my_templates")
if err then
    if err:kind() == errors.PERMISSION_DENIED then
        -- access denied
    elseif err:kind() == errors.INVALID then
        -- invalid input
    end
end

local result, err = set:render("page", {title = "Home"})
if err then
    if err:kind() == errors.NOT_FOUND then
        -- template doesn't exist
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.PERMISSION_DENIED`, `errors.INTERNAL`

## Example

```lua
local templates = require("templates")

local set, err = templates.get("app:email_templates")
if err then error(err) end

local html, err = set:render("welcome", {
    name = "Alice",
    items = {"Feature A", "Feature B", "Feature C"}
})
if err then error(err) end

print(html)

set:release()
```

**Template inheritance example:**

```lua
local set, err = templates.get("app:web_templates")
if err then error(err) end

-- Child template extends base layout
local page, err = set:render("user_profile", {
    title = "Profile",
    user = {
        name = "Bob",
        email = "bob@example.com",
        age = 30
    }
})
if err then error(err) end

set:release()
```
