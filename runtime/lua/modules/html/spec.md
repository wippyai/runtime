<!-- SPDX-License-Identifier: MPL-2.0 -->

# html

HTML sanitization with policy-based filtering. Security, deterministic.

## Loading

```lua
local html = require("html")
```

## Functions

### html.sanitize.new_policy() → Policy, error

Creates an empty policy. No elements or attributes are allowed by default.

**Returns:**
- Success: `Policy` - Empty policy object
- Error: `nil, error` - Always returns nil error (creation never fails)

### html.sanitize.ugc_policy() → Policy, error

Creates a policy for user-generated content with common formatting elements pre-configured.

**Returns:**
- Success: `Policy` - UGC policy with basic formatting allowed
- Error: `nil, error` - Always returns nil error (creation never fails)

### html.sanitize.strict_policy() → Policy, error

Creates a policy that strips all HTML tags, leaving only plain text.

**Returns:**
- Success: `Policy` - Strict policy that removes all HTML
- Error: `nil, error` - Always returns nil error (creation never fails)

## Types

### Policy

Returned by `html.sanitize.new_policy()`, `html.sanitize.ugc_policy()`, and `html.sanitize.strict_policy()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| allow_elements | (...string) | Policy | Allow specific HTML elements by name |
| allow_attrs | (...string) | AttrBuilder | Returns AttrBuilder for configuring attributes |
| allow_standard_urls | () | Policy | Enable standard URL handling with security defaults |
| require_parseable_urls | (require: boolean) | Policy | Require URLs to be parseable |
| allow_relative_urls | (allow: boolean) | Policy | Allow or disallow relative URLs |
| allow_url_schemes | (...string) | Policy | Restrict allowed URL schemes (e.g., "https", "mailto") |
| require_nofollow_on_links | (require: boolean) | Policy | Add rel="nofollow" to links |
| require_noreferrer_on_links | (require: boolean) | Policy | Add rel="noreferrer" to links |
| add_target_blank_to_fully_qualified_links | (add: boolean) | Policy | Add target="_blank" to external links |
| allow_data_uri_images | () | Policy | Allow base64 data URI images |
| allow_standard_attributes | () | Policy | Allow dir, id, lang, title attributes globally |
| allow_images | () | Policy | Allow img elements and standard image attributes |
| allow_lists | () | Policy | Allow ul, ol, dl and related list elements |
| allow_tables | () | Policy | Allow table elements and attributes |
| sanitize | (html: string) | string | Sanitize HTML string according to policy |

#### policy:allow_elements(...string) → Policy

Allow specific HTML elements.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | string | yes | - | Element names (e.g., "p", "div", "a") |

**Returns:** Policy object for method chaining

```lua
policy:allow_elements("p", "strong", "em")
```

#### policy:allow_attrs(...string) → AttrBuilder

Allow specific attributes. Returns AttrBuilder to configure where attributes are allowed.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | string | yes | - | Attribute names (e.g., "href", "class", "id") |

**Returns:** AttrBuilder for chaining with `on_elements()`, `globally()`, or `matching()`

```lua
local builder = policy:allow_attrs("href", "class")
builder:on_elements("a")
```

#### policy:allow_standard_urls() → Policy

Enable standard URL handling with security defaults.

**Returns:** Policy object for method chaining

#### policy:require_parseable_urls(require: boolean) → Policy

Require URLs to be parseable by Go's net/url package.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| require | boolean | yes | - | true to require parseable URLs |

**Returns:** Policy object for method chaining

#### policy:allow_relative_urls(allow: boolean) → Policy

Allow or disallow relative URLs in href and src attributes.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| allow | boolean | yes | - | true to allow relative URLs |

**Returns:** Policy object for method chaining

#### policy:allow_url_schemes(...string) → Policy

Restrict allowed URL schemes.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | string | yes | - | Scheme names (e.g., "https", "mailto") |

**Returns:** Policy object for method chaining

```lua
policy:allow_url_schemes("https", "mailto")
```

#### policy:require_nofollow_on_links(require: boolean) → Policy

Add rel="nofollow" attribute to all links.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| require | boolean | yes | - | true to add nofollow |

**Returns:** Policy object for method chaining

#### policy:require_noreferrer_on_links(require: boolean) → Policy

Add rel="noreferrer" attribute to all links.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| require | boolean | yes | - | true to add noreferrer |

**Returns:** Policy object for method chaining

#### policy:add_target_blank_to_fully_qualified_links(add: boolean) → Policy

Add target="_blank" to fully qualified (external) links.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| add | boolean | yes | - | true to add target="_blank" |

**Returns:** Policy object for method chaining

#### policy:allow_data_uri_images() → Policy

Allow base64-encoded data URIs in image src attributes.

**Returns:** Policy object for method chaining

#### policy:allow_standard_attributes() → Policy

Allow standard HTML attributes globally: dir, id, lang, title.

**Returns:** Policy object for method chaining

#### policy:allow_images() → Policy

Allow img elements and standard image attributes (src, alt, etc.).

**Returns:** Policy object for method chaining

#### policy:allow_lists() → Policy

Allow list elements: ul, ol, dl, li, dt, dd.

**Returns:** Policy object for method chaining

#### policy:allow_tables() → Policy

Allow table elements: table, thead, tbody, tfoot, tr, td, th, caption.

**Returns:** Policy object for method chaining

#### policy:sanitize(html: string) → string

Sanitize HTML string according to the policy configuration.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| html | string | yes | - | HTML string to sanitize |

**Returns:** Sanitized HTML string with disallowed elements/attributes removed

```lua
local clean = policy:sanitize('<p>Hello <script>alert("xss")</script></p>')
-- Returns: "Hello " (strict) or "<p>Hello </p>" (with p allowed)
```

### AttrBuilder

Returned by `policy:allow_attrs()`. Used to configure where attributes are allowed.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| on_elements | (...string) | Policy | Restrict attributes to specific elements |
| globally | () | Policy | Allow attributes on any permitted element |
| matching | (pattern: string) | AttrBuilder, error | Validate attribute values with regex pattern |

#### builder:on_elements(...string) → Policy

Restrict attributes to specific elements.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | string | yes | - | Element names where attributes are allowed |

**Returns:** Original Policy object for method chaining

```lua
policy:allow_attrs("href"):on_elements("a")
```

#### builder:globally() → Policy

Allow attributes on any permitted element.

**Returns:** Original Policy object for method chaining

```lua
policy:allow_attrs("class"):globally()
```

#### builder:matching(pattern: string) → AttrBuilder, error

Validate attribute values with a regex pattern. Call `on_elements()` or `globally()` after this to apply.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| pattern | string | yes | - | Regular expression pattern for validation |

**Returns:**
- Success: `AttrBuilder, nil` - Builder for chaining
- Error: `nil, error` - Invalid regex pattern

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid regex pattern | errors.INVALID | no |

```lua
local builder, err = policy:allow_attrs("class"):matching("^[a-zA-Z0-9_-]+$")
if err then
    error(err)
end
builder:globally()
```

## Errors

This module returns structured errors only for `matching()` method. Check kind with `errors.*` constants:

```lua
local builder, err = policy:allow_attrs("class"):matching("[invalid")
if err then
    if err:kind() == errors.INVALID then
        -- Invalid regex pattern
    end
end
```

**Possible kinds:** `errors.INVALID`

## Example

```lua
local html = require("html")

-- Custom policy with specific elements
local policy = html.sanitize.new_policy()
policy:allow_elements("p", "strong", "em", "a")
policy:allow_attrs("href"):on_elements("a")
policy:allow_attrs("class"):matching("^[a-zA-Z0-9_-]+$"):globally()
local clean = policy:sanitize('<p class="intro">Hello <strong>world</strong>!</p>')

-- UGC policy with URL restrictions
local ugc = html.sanitize.ugc_policy()
ugc:allow_url_schemes("https", "mailto")
ugc:require_nofollow_on_links(true)
ugc:require_noreferrer_on_links(true)
local safe_content = ugc:sanitize('<a href="https://example.com">Link</a>')

-- Strict policy strips all HTML
local strict = html.sanitize.strict_policy()
local text_only = strict:sanitize('<p>Hello <script>alert("xss")</script> world</p>')
-- Returns: "Hello  world"

-- Method chaining
local policy2 = html.sanitize.new_policy()
policy2:allow_elements("a", "img")
    :allow_standard_urls()
    :allow_url_schemes("https")
    :require_nofollow_on_links(true)
    :allow_images()
local result = policy2:sanitize(user_input)
```
