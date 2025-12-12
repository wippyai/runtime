-- Test: CORS rejects disallowed origins
local assert = require("assert2")

local function main()
    local http = require("http_client")

    -- Test with disallowed origin (not in cors.allow.origins config)
    local resp, err = http.post("http://localhost:8085/stream-echo", {
        body = "test",
        headers = {
            ["Origin"] = "https://evil-site.com"
        }
    })

    assert.is_nil(err, "request should not error")
    assert.eq(resp.status_code, 200, "should still return 200 (request proceeds)")

    -- CORS headers should NOT be set for disallowed origin
    local allow_origin = resp.headers["Access-Control-Allow-Origin"]
    assert.ok(allow_origin == nil or allow_origin == "",
        "Access-Control-Allow-Origin should NOT be set for disallowed origin")

    return true
end

return { main = main }
