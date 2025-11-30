local http = require("http")
local funcs = require("funcs")

local function handler()
    local res, res_err = http.response()
    local req, req_err = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Call WASM add function via funcs.call
    local result, err = funcs.call("app.api:add", 2, 3)
    if err then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({ error = err })
        return
    end

    local data = {
        message = "hello world",
        wasm_result = result,
        calculation = "2 + 3 = " .. tostring(result)
    }
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json(data)
end

return {
    handler = handler
}
