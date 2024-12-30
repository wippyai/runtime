/**
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
OK = 200,
CREATED = 201,
NO_CONTENT = 204,
BAD_REQUEST = 400,
UNAUTHORIZED = 401,
NOT_FOUND = 404,
INTERNAL_ERROR = 500
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
CHUNKED = "chunked",
SSE = "sse"
}

-- Common Error Types
httpctx.ERROR = {
PARSE_FAILED = "PARSE_FAILED",
INVALID_STATE = "INVALID_STATE",
WRITE_FAILED = "WRITE_FAILED",
STREAM_ERROR = "STREAM_ERROR"
}

-- Usage Example
function handler(args)
-- Initialize with options
local req = httpctx.request({
timeout = args.timeout,     -- optional request timeout
max_body = args.max_body    -- optional body size limit
})
local res = httpctx.response()

    -- Request operations
    if req:has_body() then
        -- Single read with error handling
        local data, err = req:read_as("json")
        if err then
            res:set_status(httpctx.STATUS.BAD_REQUEST)
            res:set_content_type(httpctx.CONTENT.JSON)
            res:write({error = err})
            return
        end
        
        -- Or streaming
        for chunk in req:body_chunks() do
            -- process chunk
        end
    end

    -- Response operations
    res:set_status(httpctx.STATUS.OK)
    res:set_content_type(httpctx.CONTENT.JSON)
    res:write({status = "ok"})

    -- Streaming if needed
    if req:accepts("text/event-stream") then
        res:set_transfer(httpctx.TRANSFER.SSE)
        res:write_event({
            name = "update",
            data = {progress = 50}
        })
    end
end
*/