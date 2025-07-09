local http = require("http")
local otel = require("otel")
local funcs = require("funcs")

local function handler(ctx, req, res)
    -- Create executor with options
    local executor = funcs.new()
    local executor_with_options = executor:with_options({
        retry = {
            attempts = 10
        },
        ratelimit = {
            rps = 1,
            burst = 1
        },
        timeout = {
            timeout = "200ms"
        }
    })

    -- Test the executor with options by calling a test function
    local result, err = executor_with_options:call("app.interceptor.demo:interceptor_demo_retry")
    if err then
        -- If there's an error, it's expected because the retry function is designed to fail
        otel.attribute("test.error", err)
        otel.event("test_failed", { error = err })
    else
        otel.attribute("test.result", result)
        otel.event("test_succeeded", { result = result })
    end

    -- Create response
    local html = [[
        <!DOCTYPE html>
        <html>
        <head>
            <title>With Options Demo</title>
            <style>
                body { font-family: Arial, sans-serif; margin: 40px; }
                h1 { color: #333; }
                .info { background: #f5f5f5; padding: 20px; border-radius: 5px; }
            </style>
        </head>
        <body>
            <h1>With Options Demo</h1>
            <div class="info">
                <p>This page demonstrates the usage of with_options().</p>
                <p>Check your tracing backend to see the span attributes and events.</p>
                <p>Test result: ]] .. (err and "Error: " .. err or "Success: " .. tostring(result)) .. [[</p>
            </div>
        </body>
        </html>
    ]]

    -- Send response
    if res then
        res:set_header("Content-Type", "text/html")
        res:write(html)
        res:flush()
    end

    -- Add response attributes and events
    otel.attribute("http.status_code", 200)
    otel.attribute("http.content_length", #html)
    otel.event("response_sent", {
        content_type = "text/html",
        content_length = #html
    })

    -- Mark span as successful
    otel.status(1, "Request processed successfully")
end

return {
    handler = handler
} 