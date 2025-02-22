local http = require("http")
local fs = require("fs")
local json = require("json")

function list_directory()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    -- Set up response
    res:set_content_type(http.CONTENT.JSON)

    -- Get default filesystem
    local myfs = fs.default()
    if not myfs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get default filesystem"
        })
        return
    end

    -- For now just return that we got filesystem
    res:set_status(http.STATUS.OK)
    res:write_json({
        status = "Got default filesystem"
    })
end

return {
    list_directory = list_directory
}