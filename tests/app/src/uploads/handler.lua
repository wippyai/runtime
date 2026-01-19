local http = require("http")
local json = require("json")
local fs = require("fs")

local function handler()
    local req = http.request()
    local res = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    local result = {}

    local form, form_err = req:parse_multipart()
    if form_err then
        result.error = "parse_multipart failed: " .. tostring(form_err)
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json(result)
        return
    end

    if not form.files or not form.files.file then
        result.error = "no file field in form"
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json(result)
        return
    end

    local file = form.files.file[1]
    if not file then
        result.error = "no file in files array"
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json(result)
        return
    end
    result.filename = file:name()
    result.size = file:size()

    local stream, stream_err = file:stream()
    if stream_err then
        result.error = "stream failed: " .. tostring(stream_err)
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json(result)
        return
    end

    local storage, fs_err = fs.get("app.uploads:storage")
    if fs_err then
        result.error = "fs.get failed: " .. tostring(fs_err)
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json(result)
        return
    end

    local dest_path = "/" .. file:name()
    local ok, write_err = storage:write_file(dest_path, stream)
    if write_err then
        result.error = "write_file failed: " .. tostring(write_err)
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json(result)
        return
    end

    result.written = ok
    result.dest_path = dest_path

    local content, read_err = storage:read_file(dest_path)
    if read_err then
        result.error = "read_file failed: " .. tostring(read_err)
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json(result)
        return
    end

    result.written_size = #content
    result.written_content = content

    res:set_status(http.STATUS.OK)
    res:write_json(result)
end

return {
    handler = handler
}
