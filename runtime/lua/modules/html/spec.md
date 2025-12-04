# Lua HTML Module Specification

## Overview

The `html` module provides HTML sanitization via bluemonday policies. Create policies to define allowlists for elements, attributes, and URL handling.

## Module Interface

### Module Loading

```lua
local html = require("html")
```

## Policy Creation

### html.sanitize.new_policy()

Creates an empty policy. Add allowlists explicitly.

Returns:

- `policy`: Policy object (or nil on error).
- `error`: Structured error object (or nil on success).

### html.sanitize.ugc_policy()

Creates a policy for user-generated content with common formatting elements.

Returns:

- `policy`: Policy object (or nil on error).
- `error`: Structured error object (or nil on success).

### html.sanitize.strict_policy()

Creates a policy that strips all HTML, leaving plain text.

Returns:

- `policy`: Policy object (or nil on error).
- `error`: Structured error object (or nil on success).

## Policy Methods

All policy methods return the policy for chaining.

### Element Control

#### policy:allow_elements(element1, element2, ...)

Allow specific HTML elements.

#### policy:allow_images()

Allow img elements and standard image attributes.

#### policy:allow_lists()

Allow ul, ol, dl and related list elements.

#### policy:allow_tables()

Allow table elements and attributes.

### Attribute Control

#### policy:allow_attrs(attr1, attr2, ...)

Returns an AttrBuilder for configuring where attributes are allowed.

#### policy:allow_standard_attributes()

Allow dir, id, lang, title attributes globally.

### URL Security

#### policy:allow_standard_urls()

Enable standard URL handling with security defaults.

#### policy:allow_url_schemes(scheme1, scheme2, ...)

Restrict allowed URL schemes.

#### policy:require_nofollow_on_links(bool)

Add rel="nofollow" to links.

#### policy:require_noreferrer_on_links(bool)

Add rel="noreferrer" to links.

#### policy:allow_relative_urls(bool)

Allow or disallow relative URLs.

#### policy:require_parseable_urls(bool)

Require URLs to be parseable.

#### policy:add_target_blank_to_fully_qualified_links(bool)

Add target="_blank" to external links.

#### policy:allow_data_uri_images()

Allow base64 data URI images.

### Sanitization

#### policy:sanitize(html_string) -> string

Sanitize HTML string according to policy.

## AttrBuilder Methods

Returned by `policy:allow_attrs()`:

#### attr_builder:on_elements(element1, element2, ...)

Restrict attributes to specific elements. Returns the original policy.

#### attr_builder:globally()

Allow attributes on any permitted element. Returns the original policy.

#### attr_builder:matching(pattern)

Validate attribute values with regex pattern.

Parameters:

- `pattern`: Regular expression pattern string.

Returns:

- `attr_builder`: AttrBuilder for chaining (or nil on error).
- `error`: Structured error object (or nil on success).

## Error Handling

### Error Types

1. **Invalid Regex Pattern:**

```lua
local builder = policy:allow_attrs("class")
local result, err = builder:matching("[invalid")
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

### Error Kind Comparison

Always use `errors.*` constants:

```lua
local result, err = builder:matching(pattern)
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid pattern
    end
end
```

## Example Usage

```lua
local html = require("html")

-- Custom policy
local policy = html.sanitize.new_policy()
policy:allow_elements("p", "strong", "em", "a")
policy:allow_attrs("href"):on_elements("a")
policy:allow_attrs("class"):matching("^[a-zA-Z0-9_-]+$"):globally()
local clean = policy:sanitize(dirty_html)

-- UGC with restrictions
local ugc = html.sanitize.ugc_policy()
ugc:allow_url_schemes("https", "mailto")
ugc:require_nofollow_on_links(true)
local clean = ugc:sanitize(user_content)

-- Strip everything
local strict = html.sanitize.strict_policy()
local text_only = strict:sanitize(html_content)

-- Method chaining
local policy = html.sanitize.new_policy()
policy:allow_elements("a", "img")
    :allow_standard_urls()
    :allow_url_schemes("https")
    :require_nofollow_on_links(true)
    :allow_images()
```

## Security Notes

- Create fresh policies per sanitization context
- Use regex patterns to validate attribute values
- Restrict URL schemes to https/mailto for user content
- Enable nofollow/noreferrer on links for UGC
- Strict policy is safest for untrusted content

## Thread Safety

- The `html` module is thread-safe.
- Module tables are immutable and shared across Lua states.
- Policy objects should be created per-use for isolation.

## Module Classification

- **Class**: `security`, `deterministic`
- Operations are pure functions with no side effects.
- Same input always produces the same output.

## Implementation Notes

- Uses `github.com/microcosm-cc/bluemonday` for sanitization.
- Module uses `ModuleDef` struct for definition.
- Type methods registered via `value.RegisterTypeMethods`.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "html",
    Description: "HTML sanitization with policy-based filtering",
    Class:       []string{luaapi.ClassSecurity, luaapi.ClassDeterministic},
    Build:       buildModule,
}
```
