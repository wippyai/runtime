local http = require("http")
local json = require("json")
local registry = require("registry")

-- Handler function to create a simple text virtual page
local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response context"
    end

    -- Get the current snapshot
    local snapshot, err = registry.snapshot()
    if not snapshot then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to get registry snapshot: " .. (err or "unknown error")
        })
        return
    end

    -- Create a changeset from the snapshot
    local changes = snapshot:changes()

    -- Get current timestamp for unique name
    local timestamp = os.time()

    -- Create a new virtual page entry in the registry
    changes:create({
        id = { ns = "fortress.pages", name = "simple-text-page-" .. timestamp },
        kind = "registry.entry",
        meta = {
            type = "virtual.page",
            name = "simple-text",
            title = "Simple Text Page",
            icon = "tabler:file-text",
            content_type = "text/html"
        },
        data = {
            source = [[
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Simple Text Page</title>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-50">
    <div class="min-h-screen flex items-center justify-center">
        <div class="max-w-md w-full bg-white shadow-lg rounded-lg p-8">
            <h1 class="text-2xl font-bold text-center mb-6">Hello from Simple Text Page</h1>
            <p class="text-gray-600 mb-4">
                This is a simple text page created via API endpoint.
            </p>
            <p class="text-gray-600 mb-4">
                Page created at: ]] .. os.date("%Y-%m-%d %H:%M:%S", timestamp) .. [[
            </p>
            <div class="text-center mt-8">
                <a href="/" class="text-indigo-600 hover:text-indigo-800">Return to home</a>
            </div>
        </div>
    </div>
</body>
</html>
        ]]
        }
    })

    -- Apply changes to create a new version
    local version, err = changes:apply()
    if not version then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to apply registry changes: " .. (err or "unknown error")
        })
        return
    end

    -- Return success response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "Simple text page created successfully",
        page_id = "fortress.pages:simple-text-page-" .. timestamp,
        version = version:id()
    })
end

return {
    handler = handler
}
