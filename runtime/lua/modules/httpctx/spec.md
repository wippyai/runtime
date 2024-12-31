# HTTP Context Module Specification

```lua
--[[
HTTP Context Module
Provides request/response handling for HTTP servers

Request constructor options:
- timeout: number (milliseconds) - Request timeout for body operations
- max_body: number (bytes) - Maximum allowed body size
]]

local httpctx = require("httpctx")

-- Core HTTP Methods
httpctx.METHOD = {
    GET = "GET",
    POST = "POST",
    PUT = "PUT",
    DELETE = "DELETE",
    PATCH = "PATCH",
    HEAD = "HEAD",
    OPTIONS = "OPTIONS"
}

-- Essential Status Codes
httpctx.STATUS = {
    OK = 200,          -- Success
    CREATED = 201,     -- Resource created
    NO_CONTENT = 204,  -- Success with no body
    BAD_REQUEST = 400, -- Client error
    UNAUTHORIZED = 401, -- Authentication required
    NOT_FOUND = 404,   -- Resource not found
    INTERNAL_ERROR = 500 -- Server error
}

-- Basic Content Types 
httpctx.CONTENT = {
    JSON = "application/json",
    FORM = "application/x-www-form-urlencoded",
    MULTIPART = "multipart/form-data",
    TEXT = "text/plain",
    STREAM = "application/octet-stream"
}

-- Transfer Types
httpctx.TRANSFER = {
    CHUNKED = "chunked", -- Chunked transfer encoding
    SSE = "sse"         -- Server-sent events
}

-- Common Error Types
httpctx.ERROR = {
    PARSE_FAILED = "PARSE_FAILED",    -- Body parsing failed
    INVALID_STATE = "INVALID_STATE",   -- Operation not valid in current state
    WRITE_FAILED = "WRITE_FAILED",     -- Response write failed
    STREAM_ERROR = "STREAM_ERROR"      -- Streaming operation failed
}

-- Usage Example with Types
function handler(args)
    -- Initialize with options (all fields optional)
    local req = httpctx.request({
        timeout = args.timeout,  -- number (milliseconds)
        max_body = args.max_body -- number (bytes) 
    })
    local res = httpctx.response()

    -- Request Methods - Basic Info
    -- All methods return nil + error_string on failure
    local method = req:method()           -- returns: string (e.g. "GET", "POST")
    local path = req:path()               -- returns: string (e.g. "/api/users")
    local query = req:query("key")        -- returns: string|nil (nil if not found)
    local header = req:header("key")      -- returns: string|nil (nil if not found)
    local ctype = req:content_type()      -- returns: string|nil (nil if not set)
    local length = req:content_length()   -- returns: number (0 if no body)
    local host = req:host()               -- returns: string (e.g. "example.com")
    local addr = req:remote_addr()        -- returns: string (e.g. "1.2.3.4:5678")
    local accepts = req:accepts("type")   -- returns: boolean

    -- Content Type Checking
    -- Compare against httpctx.CONTENT values
    local is_json = req:is_content_type(httpctx.CONTENT.JSON)  -- returns: boolean
    
    -- Body Operations
    -- All body operations respect timeout and max_body settings
    if req:has_body() then  -- returns: boolean
        if req:is_content_type(httpctx.CONTENT.JSON) then
            -- Parse body as JSON
            local json, err = req:body_json()  -- returns: table|nil, error_string|nil
            if err then
                res:set_status(httpctx.STATUS.BAD_REQUEST)
                res:set_content_type(httpctx.CONTENT.JSON)
                res:write_json({error = err})
                return
            end
        else
            -- Get raw body
            local body, err = req:body()  -- returns: string|nil, error_string|nil
            if err then
                res:set_status(httpctx.STATUS.BAD_REQUEST)
                res:set_content_type(httpctx.CONTENT.JSON)
                res:write_json({error = err})
                return
            end
        end

        -- Or stream the body for large requests
        -- Returns iterator yielding (string|nil, error_string|nil)
        -- Each chunk is of undefined size but <= max_body
        for chunk, err in req:stream_body() do
            if err then
                -- Handle streaming error
                break
            end
            -- Process chunk (string)
        end
    end

    -- Response Operations
    -- Status must be set before writing body
    res:set_status(httpctx.STATUS.OK)  -- returns: error_string|nil
    
    -- Headers must be set before writing body
    res:set_header("X-Custom", "value")  -- returns: error_string|nil
    res:set_content_type(httpctx.CONTENT.JSON)  -- returns: error_string|nil
    
    -- Write operations
    res:write("raw data")  -- returns: error_string|nil
    res:write_json({      -- returns: error_string|nil
        status = "ok",    -- accepts any table, will be JSON encoded
        data = {          -- nested tables are supported
            field = "value"
        }
    })  

    -- Chunked Transfer-Encoding
    -- Useful for streaming large responses or when content length is unknown
    res:set_transfer(httpctx.TRANSFER.CHUNKED)  -- returns: error_string|nil
    
    -- Write chunks
    local chunks = {"part1", "part2", "part3"}
    for _, chunk in ipairs(chunks) do
        local err = res:write(chunk)  -- Each write sends a separate chunk
        if err then
            -- Handle write error
            break
        end
    end

    -- Server-Sent Events (SSE)
    if req:accepts("text/event-stream") then
        -- Must be set before writing events
        res:set_transfer(httpctx.TRANSFER.SSE)  -- returns: error_string|nil
        
        -- Write SSE event
        -- name: required string
        -- data: required table (will be JSON encoded)
        res:write_event({  -- returns: error_string|nil
            name = "update",
            data = { 
                progress = 50 
            }
        })
    end
end
```