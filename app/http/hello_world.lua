local http = require("http")
local funcs = require("funcs")

function handler()
    local res = http.response()
    local req = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Get optional name parameter
    local name = req:query("name")

    -- Create function executor and call hello world function
    local executor = funcs.new()
    local result, err = executor:call("functions:hello.greet", name)

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write("Error: " .. err)
        return
    end

    -- Send response
    res:set_content_type(http.CONTENT.TEXT)
    res:set_status(http.STATUS.OK)
    res:write(result)
end

return {
    handler = handler
}