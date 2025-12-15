-- Test: HTTP response headersSent tracking via test endpoint
local assert = require("assert2")

local function main()
    local http = require("http_client")

    -- Test 1: Normal flow - headers before write (should succeed)
    local resp1, err1 = http.get("http://localhost:8085/test/response-headers?test=normal")
    assert.is_nil(err1, "normal test should not error")
    assert.eq(resp1.status_code, 200, "status 200")
    -- Check custom header was set
    assert.eq(resp1.headers["X-Custom"], "test-value", "custom header set")

    -- Test 2: Write then set_status (should error internally but not crash)
    local resp2, err2 = http.get("http://localhost:8085/test/response-headers?test=write_then_status")
    assert.is_nil(err2, "write_then_status should not crash")
    -- The response should still be sent (the write happened)
    assert.eq(resp2.body, "some data", "body was written")

    -- Test 3: Write then set_header (should error internally)
    local resp3, err3 = http.get("http://localhost:8085/test/response-headers?test=write_then_header")
    assert.is_nil(err3, "write_then_header should not crash")
    assert.eq(resp3.body, "some data", "body was written")

    -- Test 4: Write then set_content_type (should error internally)
    local resp4, err4 = http.get("http://localhost:8085/test/response-headers?test=write_then_content_type")
    assert.is_nil(err4, "write_then_content_type should not crash")
    assert.eq(resp4.body, "some data", "body was written")

    -- Test 5: Write then set_transfer (should error internally)
    local resp5, err5 = http.get("http://localhost:8085/test/response-headers?test=write_then_transfer")
    assert.is_nil(err5, "write_then_transfer should not crash")
    assert.eq(resp5.body, "some data", "body was written")

    -- Test 6: SSE auto-headers
    local resp6, err6 = http.get("http://localhost:8085/test/response-headers?test=sse_auto")
    assert.is_nil(err6, "sse_auto should not error")
    -- SSE sets content-type automatically
    assert.ok(resp6.headers["Content-Type"]:find("text/event%-stream"), "SSE content type set")

    return true
end

return { main = main }
