local json = require("json")
local http = require("http")

local function handler()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        message = "pong",
        module = "wippy/dummy"
    })
end

return { handler = handler }
