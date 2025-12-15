-- Test: CORS wildcard subdomain matching
local assert = require("assert2")

local function main()
    local http = require("http_client")

    -- Test wildcard subdomain matching (*.example.com pattern)
    -- This tests that app.example.com matches *.example.com
    local test_origins = {
        "https://app.example.com",
        "https://api.example.com",
        "https://dashboard.example.com"
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
