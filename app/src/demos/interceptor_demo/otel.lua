local http = require("http")
local otel = require("otel")
local child_spans = require("child_spans")

local function handler(ctx, req, res)
    -- Create response
    local html = [[
        <!DOCTYPE html>
        <html>
        <head>
            <title>OpenTelemetry Demo</title>
            <style>
                body { font-family: Arial, sans-serif; margin: 40px; }
                h1 { color: #333; }
                .info { background: #f5f5f5; padding: 20px; border-radius: 5px; }
            </style>
        </head>
        <body>
            <h1>OpenTelemetry Demo</h1>
            <div class="info">
                <p>This page demonstrates OpenTelemetry instrumentation.</p>
                <p>Check your tracing backend to see the span attributes and events.</p>
            </div>
        </body>
        </html>
    ]]

    -- Process the HTML content with child spans
    local processed_html = child_spans.process(ctx, html)

    -- Send response
    if res then
        res:set_header("Content-Type", "text/html")
        res:write(processed_html)
        res:flush()
    end

    -- Add response attributes and events
    otel.attribute("http.status_code", 200)
    otel.attribute("http.content_length", #processed_html)
    otel.event("response_sent", {
        content_type = "text/html",
        content_length = #processed_html
    })

    -- Mark span as successful
    otel.status(1, "Request processed successfully")
end

return {
    handler = handler
}