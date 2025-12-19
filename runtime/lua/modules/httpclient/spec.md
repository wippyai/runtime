# http_client

HTTP client for making requests (GET, POST, etc.). Network, io, nondeterministic.

## Loading

```lua
local http_client = require("http_client")
```

## Dependencies

### Stream (from stream module)

Returned by requests with `stream = true` option.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| read | (size?: integer) | string, error | yields until data read or EOF |
| close | () | boolean, error | yields until closed |

See: `wippy/runtime/lua/modules/stream/`

## Functions

### get(url: string, options?: table) → Response, error

HTTP GET request.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| url | string | yes | - | HTTP/HTTPS URL |
| options | table | no | nil | Request options (see below) |

**Yields:** until response received or timeout

**Returns:** Response table or `nil, error`

**Response table fields:**

| Field | Type | Notes |
|-------|------|-------|
| status_code | integer | HTTP status code |
| url | string | Final URL (after redirects) |
| body | string | Response body (if not streaming) |
| body_size | integer | Body size in bytes (-1 if streaming) |
| headers | table | Response headers {[name]: value} |
| cookies | table | Response cookies {[name]: value} |
| stream | Stream | Stream object (only if `stream = true`) |

**Errors (strings):**
- `"no context"` - missing execution context
- `"not allowed: {url}"` - security policy denied
- Network/timeout errors from HTTP layer

### post(url: string, options?: table) → Response, error

HTTP POST request.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| url | string | yes | - | HTTP/HTTPS URL |
| options | table | no | nil | Request options (see below) |

**Yields:** until response received or timeout

**Returns:** Response table or `nil, error`

**Errors:** Same as `get()`

### put(url: string, options?: table) → Response, error

HTTP PUT request. Same signature and behavior as `post()`.

### delete(url: string, options?: table) → Response, error

HTTP DELETE request. Same signature and behavior as `get()`.

### head(url: string, options?: table) → Response, error

HTTP HEAD request. Same signature and behavior as `get()`.

### patch(url: string, options?: table) → Response, error

HTTP PATCH request. Same signature and behavior as `post()`.

### request(method: string, url: string, options?: table) → Response, error

Generic HTTP request with custom method.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| method | string | yes | - | HTTP method (GET, POST, etc.) |
| url | string | yes | - | HTTP/HTTPS URL |
| options | table | no | nil | Request options (see below) |

**Yields:** until response received or timeout

**Returns:** Response table or `nil, error`

**Errors:** Same as `get()`

### request_batch(requests: table) → table, table|nil

Execute multiple HTTP requests concurrently.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| requests | table | yes | - | Array of request definitions |

**Request definition format:** Each entry is `{method, url, options?}`

| Index | Type | Required | Notes |
|-------|------|----------|-------|
| [1] | string | yes | HTTP method |
| [2] | string | yes | URL |
| [3] | table | no | Request options (stream not supported) |

**Yields:** until all requests complete

**Returns:**
- Success (all requests succeeded): `responses, nil` - array of Response tables
- Partial failure: `responses, errors` - responses[i] is Response or nil, errors[i] is error string or nil
- Parse error: `nil, error` - invalid request format

**Errors (strings):**
- `"no context"` - missing execution context
- `"requests table cannot be empty"` - empty requests array
- `"each request must be a table"` - invalid request format
- `"method must be a string"` - missing/invalid method
- `"URL must be a string"` - missing/invalid URL
- `"not allowed: {url}"` - security policy denied
- `"streaming not supported in batch requests"` - stream option used
- Individual request errors returned in errors array

**Notes:**
- Streaming (`stream = true`) is not supported in batch requests
- Requests execute concurrently
- Result arrays are 1-indexed matching request order

### encode_uri(s: string) → string

URL-encodes a string for use in query parameters.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| s | string | yes | - | String to encode |

**Returns:** URL-encoded string

**Notes:**
- Spaces encoded as `+`
- Special characters percent-encoded

### decode_uri(s: string) → string, error

Decodes a URL-encoded string.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| s | string | yes | - | URL-encoded string |

**Returns:** Decoded string or `nil, error`

**Errors (strings):**
- `"invalid URL escape..."` - malformed encoding

## Request Options

The `options` table supports the following fields:

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| headers | table | nil | Request headers {[name]: value} |
| body | string | nil | Request body |
| timeout | integer\|string | 0 | Timeout: integer = seconds, string = Go duration ("5s", "1m30s") |
| query | table | nil | Query parameters {[name]: value} |
| cookies | table | nil | Cookies {[name]: value} |
| form | table | nil | Form data {[name]: value} (sets Content-Type to application/x-www-form-urlencoded) |
| files | table | nil | File uploads (array of file definitions, see below) |
| auth | table | nil | Basic auth: {user: string, pass: string} |
| stream | boolean | false | Return stream object instead of buffering body |
| max_response_body | integer | 0 | Max response size in bytes (0 = default limit) |
| unix_socket | string | nil | Unix socket path (requires security permission) |

**File definition format:**

| Field | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Form field name |
| filename | string | yes | - | Filename |
| content | string | no | - | File content (either content or reader required) |
| reader | userdata | no | - | io.Reader for file content |
| content_type | string | no | "application/octet-stream" | MIME type |

**Timeout formats:**
- Integer: seconds (e.g., `30` = 30 seconds)
- String: Go duration format (e.g., `"5s"`, `"1m30s"`, `"1h"`)

## Errors

This module returns string errors, not structured errors.

Error handling pattern:

```lua
local resp, err = http_client.get(url)
if err then
    -- err is a string
    print("Error:", err)
    return nil, err
end
```

Common error strings:
- `"no context"` - internal error
- `"not allowed: {url}"` - security policy denied request
- `"not allowed: unix socket {path}"` - security policy denied unix socket
- `"not allowed: private IP {ip}"` - SSRF protection blocked private IP
- Network errors (DNS, connection, timeout, etc.)
- HTTP protocol errors

## Example

```lua
local http_client = require("http_client")
local json = require("json")

-- Simple GET
local resp, err = http_client.get("https://api.example.com/data")
if err then error(err) end
print(resp.status_code)
print(resp.body)

-- POST with JSON
local payload = json.encode({name = "test", value = 42})
local resp2, err2 = http_client.post("https://api.example.com/submit", {
    headers = {["Content-Type"] = "application/json"},
    body = payload,
    timeout = "30s"
})
if err2 then error(err2) end

-- GET with query parameters
local resp3, err3 = http_client.get("https://api.example.com/search", {
    query = {q = "lua", limit = "10"},
    headers = {["Authorization"] = "Bearer token123"}
})
if err3 then error(err3) end

-- File upload
local resp4, err4 = http_client.post("https://api.example.com/upload", {
    form = {title = "My Document", description = "Test file"},
    files = {
        {
            name = "attachment",
            filename = "test.txt",
            content = "file content here",
            content_type = "text/plain"
        }
    }
})
if err4 then error(err4) end

-- Streaming response
local resp5, err5 = http_client.get("https://api.example.com/large-file", {
    stream = true
})
if err5 then error(err5) end

while true do
    local chunk, read_err = resp5.stream:read(4096)
    if read_err or chunk == nil then break end
    -- process chunk
end
resp5.stream:close()

-- Batch requests
local responses, errors = http_client.request_batch({
    {"GET", "https://api.example.com/users"},
    {"GET", "https://api.example.com/posts"},
    {"POST", "https://api.example.com/log", {
        body = "event data"
    }}
})

if errors then
    for i, err in ipairs(errors) do
        if err then
            print("Request", i, "failed:", err)
        end
    end
else
    -- all succeeded
    for i, resp in ipairs(responses) do
        print("Request", i, "status:", resp.status_code)
    end
end
```
