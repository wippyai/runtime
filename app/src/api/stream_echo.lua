local http = require("http")

local function handler()
    local req, req_err = http.request()
    local res, res_err = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Get request body as stream
    local stream, stream_err = req:stream()
    if stream_err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write("no body: " .. tostring(stream_err))
        return
    end

    -- First, read ALL data from request stream
    local chunks = {}
    while true do
        local chunk, read_err = stream:read(1024)
        if read_err then
            break
        end
        if chunk == nil then
            break
        end
        table.insert(chunks, chunk)
    end
    stream:close()

    -- Then write all data back
    res:set_transfer(http.TRANSFER.CHUNKED)
    res:set_content_type(http.CONTENT.STREAM)
    res:set_status(http.STATUS.OK)

    for _, chunk in ipairs(chunks) do
        res:write(chunk)
    end
end

return {
    handler = handler
}
