-- Test: CORS localhost origin matching with different ports
local assert = require("assert2")

local function main()
    local http = require("http_client")

    -- Test localhost with different ports
    local test_origins = {
        "http://localhost:3000",
        "http://localhost:5173",
        "http://localhost:8080",
        "https://localhost:3000",
        "http://localhost"
    }

    for _, origin in ipairs(test_origins) do
        local resp, err = http.post("http://localhost:8085/stream-echo", {
            body = "test",
            headers = {
                ["Origin"] = origin
            }
        })

        assert.is_nil(err, "request with origin " .. origin .. " should not error")

        local allow_origin = resp.headers["Access-Control-Allow-Origin"]
        assert.ok(allow_origin ~= nil and allow_origin ~= "",
            "Access-Control-Allow-Origin should be set for " .. origin)
        assert.eq(allow_origin, origin, "should echo back origin " .. origin)
    end

    return true
end

return { main = main }
