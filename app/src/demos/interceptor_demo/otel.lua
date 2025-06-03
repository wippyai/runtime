local http = require("http")

-- Main handler function
local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Create a simple HTML response
    local html = [[
        <!DOCTYPE html>
        <html>
        <head>
            <title>Interceptor Demo</title>
            <style>
                body {
                    font-family: Arial, sans-serif;
                    margin: 40px;
                    line-height: 1.6;
                }
                .container {
                    max-width: 800px;
                    margin: 0 auto;
                    padding: 20px;
                    border: 1px solid #ddd;
                    border-radius: 5px;
                }
                h1 {
                    color: #333;
                }
            </style>
        </head>
        <body>
            <div class="container">
                <h1>Interceptor Demo Page</h1>
                <p>This is a demo page showing the OpenTelemetry interceptor in action.</p>
                <p>The page is being served with tracing enabled.</p>
            </div>
        </body>
        </html>
    ]]

    -- Set the HTML content
    res:set_content_type("text/html")
    res:write(html)

    -- Set up response status
    res:set_status(http.STATUS.OK)

    -- Ensure the response is sent
    res:flush()
end

return {
    handler = handler
}