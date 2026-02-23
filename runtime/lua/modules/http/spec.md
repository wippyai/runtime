<!-- SPDX-License-Identifier: MPL-2.0 -->

# http

HTTP request and response types for server-side handlers. Network, IO, nondeterministic.

## Loading

```lua
local http = require("http")
```

## Constants

```lua
-- HTTP Methods
http.METHOD.GET                    -- "GET"
http.METHOD.POST                   -- "POST"
http.METHOD.PUT                    -- "PUT"
http.METHOD.DELETE                 -- "DELETE"
http.METHOD.PATCH                  -- "PATCH"
http.METHOD.HEAD                   -- "HEAD"
http.METHOD.OPTIONS                -- "OPTIONS"

-- HTTP Status Codes
http.STATUS.OK                     -- 200
http.STATUS.CREATED                -- 201
http.STATUS.ACCEPTED               -- 202
http.STATUS.NO_CONTENT             -- 204
http.STATUS.PARTIAL_CONTENT        -- 206
http.STATUS.MOVED_PERMANENTLY      -- 301
http.STATUS.FOUND                  -- 302
http.STATUS.SEE_OTHER              -- 303
http.STATUS.NOT_MODIFIED           -- 304
http.STATUS.TEMPORARY_REDIRECT     -- 307
http.STATUS.PERMANENT_REDIRECT     -- 308
http.STATUS.BAD_REQUEST            -- 400
http.STATUS.UNAUTHORIZED           -- 401
http.STATUS.PAYMENT_REQUIRED       -- 402
http.STATUS.FORBIDDEN              -- 403
http.STATUS.NOT_FOUND              -- 404
http.STATUS.METHOD_NOT_ALLOWED     -- 405
http.STATUS.NOT_ACCEPTABLE         -- 406
http.STATUS.CONFLICT               -- 409
http.STATUS.GONE                   -- 410
http.STATUS.UNPROCESSABLE          -- 422
http.STATUS.TOO_MANY_REQUESTS      -- 429
http.STATUS.INTERNAL_ERROR         -- 500
http.STATUS.NOT_IMPLEMENTED        -- 501
http.STATUS.BAD_GATEWAY            -- 502
http.STATUS.SERVICE_UNAVAILABLE    -- 503
http.STATUS.GATEWAY_TIMEOUT        -- 504
http.STATUS.VERSION_NOT_SUPPORTED  -- 505

-- Content Types
http.CONTENT.JSON                  -- "application/json"
http.CONTENT.FORM                  -- "application/x-www-form-urlencoded"
http.CONTENT.MULTIPART             -- "multipart/form-data"
http.CONTENT.TEXT                  -- "text/plain"
http.CONTENT.STREAM                -- "application/octet-stream"

-- Transfer Modes
http.TRANSFER.CHUNKED              -- "chunked"
http.TRANSFER.SSE                  -- "sse"

-- Error Types
http.ERROR.PARSE_FAILED            -- "PARSE_FAILED"
http.ERROR.INVALID_STATE           -- "INVALID_STATE"
http.ERROR.WRITE_FAILED            -- "WRITE_FAILED"
http.ERROR.STREAM_ERROR            -- "STREAM_ERROR"
```

## Dependencies

### stream

Used by `request:stream()` and `multipart_file:stream()` for streaming request bodies.

See: `runtime/lua/modules/stream/spec.md`

## Functions

### request(options?: table) → Request, error

Creates a Request object from current HTTP context.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | nil | Configuration options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| timeout | integer | 0 | Timeout in milliseconds for body reads |
| max_body | integer | 125829120 | Maximum body size in bytes (default 120MB) |

**Returns:**
- Success: `Request, nil` - Request object and nil error
- Error: `nil, error` - nil and structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context available | errors.INTERNAL | no |
| no HTTP request context | errors.INTERNAL | no |

### response() → Response, error

Creates a Response object from current HTTP context.

**Returns:**
- Success: `Response, nil` - Response object and nil error
- Error: `nil, error` - nil and structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context available | errors.INTERNAL | no |
| no HTTP request context | errors.INTERNAL | no |

## Types

### Request

Returned by `http.request()`. Provides access to incoming HTTP request data.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| method | () | string, error | HTTP method (GET, POST, etc) |
| path | () | string, error | Request path |
| query | (key: string) | string?, error | Single query parameter value |
| query_params | () | table, error | All query parameters as table |
| header | (name: string) | string?, error | Header value, multiple values joined with ", " |
| content_type | () | string?, error | Content-Type header value |
| content_length | () | number, error | Content-Length as number |
| host | () | string, error | Host header value |
| remote_addr | () | string, error | Remote address (IP:port) |
| body | () | string, error | Full request body as string |
| body_json | () | any, error | Request body parsed as JSON |
| has_body | () | boolean, error | Whether request has body |
| accepts | (content_type: string) | boolean, error | Check if Accept header matches |
| is_content_type | (expected: string) | boolean, error | Check if Content-Type starts with expected |
| param | (name: string) | string?, error | Single route parameter value |
| params | () | table, error | All route parameters as table |
| stream | () | Stream, error | Request body as stream |
| parse_multipart | (max_memory?: integer) | table, error | Parse multipart form data |

#### request:method() → string, error

Returns HTTP request method.

**Returns:** `string, error` - method name (e.g. "GET", "POST") or nil + error

#### request:path() → string, error

Returns request URL path.

**Returns:** `string, error` - URL path (e.g. "/api/users") or nil + error

#### request:query(key: string) → string?, error

Returns single query parameter value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Query parameter name |

**Returns:** `string?, error` - parameter value or nil if not present, plus error

#### request:query_params() → table, error

Returns all query parameters as a table. Multiple values are joined with commas.

**Returns:** `table, error` - table of key-value pairs or nil + error

#### request:header(name: string) → string?, error

Returns request header value. Multiple header values are joined with ", ".

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Header name (case-sensitive) |

**Returns:** `string?, error` - header value or nil if not present, plus error

#### request:content_type() → string?, error

Returns Content-Type header value.

**Returns:** `string?, error` - content type or nil if not set, plus error

#### request:content_length() → number, error

Returns Content-Length header value as number.

**Returns:** `number, error` - content length in bytes or nil + error

#### request:host() → string, error

Returns Host header value.

**Returns:** `string, error` - host value or nil + error

#### request:remote_addr() → string, error

Returns remote client address (IP:port format).

**Returns:** `string, error` - remote address or nil + error

#### request:body() → string, error

Reads and returns full request body as string. Enforces max_body limit and optional timeout.

**Returns:**
- Success: `string, nil` - body content as string
- Error: `nil, error` - nil and structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no body | errors.INVALID | no |
| body too large | errors.INVALID | no |
| read timeout | errors.INTERNAL | no |
| read failed | errors.INTERNAL | no |

**Notes:**
- Body can only be read once
- Default max_body is 120MB (125829120 bytes)
- Timeout applies to entire read operation

#### request:body_json() → any, error

Reads request body and parses as JSON. Enforces max_body limit.

**Returns:**
- Success: `table|string|number|boolean, nil` - parsed JSON value
- Error: `nil, error` - nil and structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no body | errors.INVALID | no |
| body too large | errors.INVALID | no |
| invalid JSON | errors.INVALID | no |
| read failed | errors.INTERNAL | no |

**Notes:**
- Body can only be read once
- Does not enforce Content-Type header

#### request:has_body() → boolean, error

Checks if request has a body (ContentLength > 0).

**Returns:** `boolean, error` - true if body present, false otherwise, plus error

#### request:accepts(content_type: string) → boolean, error

Checks if Accept header matches given content type or includes "*/*".

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| content_type | string | yes | - | Content type to check |

**Returns:** `boolean, error` - true if accepted, false otherwise, plus error

#### request:is_content_type(expected: string) → boolean, error

Checks if Content-Type header starts with expected value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| expected | string | yes | - | Expected content type prefix |

**Returns:** `boolean, error` - true if matches, false otherwise, plus error

#### request:param(name: string) → string?, error

Returns single route parameter value extracted from URL path.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Route parameter name |

**Returns:** `string?, error` - parameter value or nil if not present, plus error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no route parameters | errors.INTERNAL | no |

#### request:params() → table, error

Returns all route parameters as a table.

**Returns:** `table, error` - table of parameter key-value pairs or nil + error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no route parameters | errors.INTERNAL | no |

#### request:stream() → Stream, error

Returns request body as a stream for reading.

**Returns:** `Stream, error` - stream object or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no body | errors.INVALID | no |
| no resource table | errors.INTERNAL | no |

**Notes:**
- Returns stream.Stream object (see stream module spec)
- Use for large bodies to avoid loading entirely into memory
- Stream size is set to ContentLength

#### request:parse_multipart(max_memory?: integer) → table, error

Parses multipart/form-data request body.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| max_memory | integer | no | 33554432 | Max memory in bytes for form parsing (32MB) |

**Returns:** `table, error` - form data table or nil + structured error

**Return table structure:**

```lua
{
  files = {
    [field_name] = { MultipartFile, ... }
  },
  values = {
    [field_name] = string | string[]
  }
}
```

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| not multipart/form-data | errors.INVALID | no |
| parse failed | errors.INTERNAL | no |

**Notes:**
- Content-Type must be "multipart/form-data"
- Files are stored as MultipartFile objects
- Values are strings; multiple values become arrays

### Response

Returned by `http.response()`. Provides methods to build and send HTTP responses.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| set_status | (code: integer) | error | Set HTTP status code |
| set_header | (name: string, value: string) | error | Set response header |
| set_content_type | (content_type: string) | error | Set Content-Type header |
| write | (data: string) | error | Write response body |
| write_json | (value: any) | error | Encode value as JSON and write |
| flush | () | error | Flush buffered response data |
| set_transfer | (mode: string) | error | Set transfer mode (chunked or sse) |
| write_event | (event: table) | error | Write Server-Sent Event |

#### response:set_status(code: integer) → error

Sets HTTP response status code.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| code | integer | yes | - | HTTP status code (e.g. 200, 404) |

**Returns:** `error?` - nil on success, structured error if headers already sent

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| headers already sent | errors.INVALID | no |

**Notes:**
- Must be called before any write operations
- Automatically marks response as handled

#### response:set_header(name: string, value: string) → error

Sets a response header.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Header name |
| value | string | yes | - | Header value |

**Returns:** `error?` - nil on success, structured error if headers already sent

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| headers already sent | errors.INVALID | no |

**Notes:**
- Must be called before any write operations
- Automatically marks response as handled

#### response:set_content_type(content_type: string) → error

Sets Content-Type header.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| content_type | string | yes | - | Content type value |

**Returns:** `error?` - nil on success, structured error if headers already sent

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| headers already sent | errors.INVALID | no |

**Notes:**
- Convenience method for `set_header("Content-Type", value)`
- Must be called before any write operations

#### response:write(data: string) → error

Writes data to response body.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Data to write |

**Returns:** `error?` - nil on success, structured error on write failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| write failed | errors.INTERNAL | no |

**Notes:**
- Automatically sends headers on first write
- Marks response as handled

#### response:write_json(value: any) → error

Encodes value as JSON and writes to response body.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| value | any | yes | - | Value to encode as JSON |

**Returns:** `error?` - nil on success, structured error on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| JSON encoding failed | errors.INVALID | no |
| write failed | errors.INTERNAL | no |

**Notes:**
- Automatically sets Content-Type to "application/json" if headers not sent
- Marks response as handled

#### response:flush() → error

Flushes buffered response data to client.

**Returns:** `error?` - always nil

**Notes:**
- Useful for streaming responses
- Marks headers as sent
- No-op if underlying writer doesn't support flushing

#### response:set_transfer(mode: string) → error

Sets transfer encoding mode.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| mode | string | yes | - | Transfer mode: http.TRANSFER.CHUNKED or http.TRANSFER.SSE |

**Returns:** `error?` - nil on success, structured error if headers already sent

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| headers already sent | errors.INVALID | no |

**Notes:**
- "chunked": Sets Transfer-Encoding: chunked
- "sse": Sets Content-Type: text/event-stream and Connection: keep-alive
- Must be called before write operations

#### response:write_event(event: table) → error

Writes a Server-Sent Event (SSE).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| event | table | yes | - | Event data with name and data fields |

**Event table structure:**

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| name | string | yes | Event name |
| data | any | yes | Event data (will be JSON encoded) |

**Returns:** `error?` - nil on success, structured error on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| SSE mode after headers sent | errors.INVALID | no |
| missing event name | errors.INVALID | no |
| missing event data | errors.INVALID | no |
| JSON encoding failed | errors.INVALID | no |
| write failed | errors.INTERNAL | no |

**Notes:**
- Automatically switches to SSE mode on first call if headers not sent
- Automatically flushes after each event
- Format: `event: <name>\ndata: <json>\n\n`

### MultipartFile

Returned by `request:parse_multipart()` for uploaded files.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| name | () | string, error | Original filename |
| size | () | number, error | File size in bytes |
| header | (name: string) | string? | File-specific header value |
| stream | () | Stream, error | File content as stream |

#### multipart_file:name() → string, error

Returns original filename from upload.

**Returns:** `string, error` - filename or nil + error

#### multipart_file:size() → number, error

Returns file size in bytes.

**Returns:** `number, error` - size in bytes or nil + error

#### multipart_file:header(name: string) → string?

Returns file-specific header value from multipart section.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Header name |

**Returns:** `string?` - header value or nil if not present

**Notes:**
- Common headers: "Content-Type", "Content-Disposition"
- Returns only a single value (not an error tuple)

#### multipart_file:stream() → Stream, error

Returns file content as a stream.

**Returns:** `Stream, error` - stream object or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| file open failed | errors.INTERNAL | no |
| no resource table | errors.INTERNAL | no |

**Notes:**
- Returns stream.Stream object (see stream module spec)
- Stream size is set to file size

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local req, err = http.request()
if err then
    if err:kind() == errors.INTERNAL then
        -- Internal server error
    elseif err:kind() == errors.INVALID then
        -- Invalid request (bad input, constraints violated)
    end
end
```

**Possible kinds:** `errors.INTERNAL`, `errors.INVALID`

## Example

```lua
local http = require("http")

local function handler()
    local req, err = http.request()
    if err then error(err) end

    local res, err = http.response()
    if err then error(err) end

    -- Handle different request methods
    local method = req:method()
    if method == http.METHOD.GET then
        -- Query parameters
        local name = req:query("name") or "World"
        res:set_status(http.STATUS.OK)
        res:write_json({greeting = "Hello, " .. name})

    elseif method == http.METHOD.POST then
        -- JSON body
        local data, err = req:body_json()
        if err then
            res:set_status(http.STATUS.BAD_REQUEST)
            res:write_json({error = "Invalid JSON"})
            return
        end

        res:set_status(http.STATUS.CREATED)
        res:write_json({received = data})

    else
        res:set_status(http.STATUS.METHOD_NOT_ALLOWED)
        res:write_json({error = "Method not allowed"})
    end
end

return {handler = handler}
```

### SSE Example

```lua
local http = require("http")

local function handler()
    local res = http.response()

    -- Stream multiple events
    res:write_event({name = "start", data = {status = "beginning"}})
    res:write_event({name = "progress", data = {percent = 50}})
    res:write_event({name = "complete", data = {status = "done"}})
end

return {handler = handler}
```

### Multipart Upload Example

```lua
local http = require("http")

local function handler()
    local req = http.request()
    local res = http.response()

    -- Parse multipart form
    local form, err = req:parse_multipart()
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = tostring(err)})
        return
    end

    -- Access uploaded file
    if form.files.upload then
        local file = form.files.upload[1]
        local filename = file:name()
        local size = file:size()

        -- Get file stream
        local stream, err = file:stream()
        if err then error(err) end

        -- Process file...
        stream:close()

        res:set_status(http.STATUS.OK)
        res:write_json({
            filename = filename,
            size = size
        })
    else
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "No file uploaded"})
    end
end

return {handler = handler}
```
