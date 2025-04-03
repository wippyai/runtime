# Lua Templates Module Specification

## Overview

The `templates` module provides a Lua interface to the Jet template engine. It allows for accessing template sets and rendering templates with variables. The module is designed to be used within a unit of work context for automatic resource management and cleanup.

## Module Interface

### Loading the Module

```lua
local templates = require("templates")
```

## Core Concepts

### Resource Management

- Template sets are obtained from the resource registry
- All template resources are automatically cleaned up when the containing unit of work completes
- Template resources can be explicitly released earlier if needed using the `release` method

### Error Handling

- Operations return appropriate results and error values
- Success is indicated by a result value + nil for error
- Failure is indicated by nil + error message
- Template connections are managed as resources with proper cleanup

## Template Operations

### Getting a Template Set

```lua
local templateSet = templates.get("resource_id")
-- Parameters: resource_id (string) - Resource ID for the template set
-- Returns: template set object
-- Raises error on failure
```

### Render Template

```lua
local result, err = templateSet:render(name, variables)
-- Parameters:
--   name (string): Template name to render
--   variables (table, optional): Variables to use in rendering
-- Returns on success: rendered content (string), nil
-- Returns on error: nil, error message
```

### Release Template Set

```lua
local success = templateSet:release()
-- Returns: true (always succeeds)
-- Note: After release, template methods will fail
```

## Example Usage

### Basic Template Rendering

```lua
-- Get template set from resource registry
local templates = require("templates")
local tmpl = templates.get("app:my_templates")

-- Render a simple template
local content, err = tmpl:render("welcome", {
    name = "John Doe"
})
if err then
    error("Failed to render template: " .. err)
end

print(content)  -- "Hello, John Doe!"

-- Release the template set when done
tmpl:release()
```

### Rendering with Complex Data

```lua
local templates = require("templates")
local tmpl = templates.get("app:my_templates")

-- Render a template with complex nested data
local content, err = tmpl:render("user_profile", {
    user = {
        name = "Alice Smith",
        age = 30,
        contact = {
            email = "alice@example.com",
            phone = "555-1234"
        },
        roles = {"admin", "editor", "user"}
    },
    company = "ACME Inc.",
    showDetails = true
})
if err then
    error("Failed to render template: " .. err)
end

print(content)

-- Template sets are automatically released when the unit of work completes,
-- but it's good practice to release them explicitly when done
tmpl:release()
```

### Error Handling

```lua
local templates = require("templates")
local tmpl = templates.get("app:my_templates")

-- Try to render a template that might not exist
local content, err = tmpl:render("nonexistent_template", {})
if err then
    if err == "template not found" then
        print("The requested template does not exist")
    else
        print("Error rendering template: " .. err)
    end
else
    print(content)
end

-- Release the template set
tmpl:release()
```

### Template Processing in a Function

```lua
function process_template(template_id, template_name, vars)
    local templates = require("templates")
    local tmpl = templates.get(template_id)
    
    -- Template will be automatically released when the unit of work completes
    
    local content, err = tmpl:render(template_name, vars)
    if err then
        return nil, err
    end
    
    return content
end

-- Later use the function
local result, err = process_template("app:email_templates", "welcome_email", {
    user_name = "New User",
    activation_link = "https://example.com/activate?token=abc123"
})

if err then
    print("Failed to process template: " .. err)
else
    print("Generated email content:", result)
end
```

## Template Examples

The module works with Jet templates, which support various features:

### Simple Variable Substitution

```
Hello, {{ name }}!
```

### Control Structures

```
{{ if user.admin }}
  Welcome, Administrator!
{{ else }}
  Welcome, User!
{{ end }}
```

### Loops

```
<ul>
{{ range items }}
  <li>{{ . }}</li>
{{ end }}
</ul>
```

### Nested Data

```
<div class="profile">
  <h1>{{ user.name }}</h1>
  <p>Email: {{ user.contact.email }}</p>
  <p>Phone: {{ user.contact.phone }}</p>
  
  <h2>Roles:</h2>
  <ul>
    {{ range user.roles }}
      <li>{{ . }}</li>
    {{ end }}
  </ul>
</div>
```

### Global Variables

Templates can access global variables defined in the template set configuration:

```
<footer>
  © {{ currentYear }} {{ siteName }} | Version {{ version }}
</footer>
```