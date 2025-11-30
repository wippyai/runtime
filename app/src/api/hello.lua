local http = require("http")
local time = require("time")
local funcs = require("funcs")

local function handler()
    local res, res_err = http.response()
    local req, req_err = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Sleep for 10ms to test dispatcher
    time.sleep("10ms")

    -- Call WASM add function (2 + 3 = 5)
    local sum, err = funcs.new():call("app.api:add", 2, 3)

    local data = {
        message = "hello world",
        slept = "10ms",
        wasm_add = sum,
        wasm_err = err
    }
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json(data)
end

return {
    handler = handler
}
