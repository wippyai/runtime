local http = require("http")
local json = require("json")
local registry = require("registry")

local function handler()
    -- Get request and response objects
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Get query parameters for filtering
    local namespace = req:query("ns")
    local id = req:query("id")
    local search_term = req:query("find") or req:query("search")
    local format = req:query("format") or "html" -- Default to HTML

    -- Get metadata-specific search parameters
    local meta_name = req:query("meta_name")
    local meta_type = req:query("meta_type")
    local meta_tag = req:query("meta_tag")
    local meta_comment = req:query("meta_comment")

    -- Fix: If namespace is empty string, treat it as nil to avoid filtering by empty namespace
    if namespace and namespace == "" then
        namespace = nil
    end

    -- Create registry snapshot
    local snapshot, err = registry.snapshot()
    if not snapshot then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        local error_msg = {
            error = "Failed to get registry snapshot",
            details = err
        }

        if format == "json" then
            res:set_content_type(http.CONTENT.JSON)
            res:write_json(error_msg)
        else
            res:set_content_type(http.CONTENT.TEXT)                    -- Use TEXT as base content type
            res:set_header("Content-Type", "text/html; charset=utf-8") -- Override with HTML
            res:write(string.format([[
                <!DOCTYPE html>
                <html>
                <head>
                    <title>Registry Error</title>
                    <style>
                        body { font-family: Arial, sans-serif; margin: 2rem; }
                        .error { color: red; padding: 1rem; border: 1px solid red; background-color: #fff0f0; }
                    </style>
                </head>
                <body>
                    <h1>Registry Error</h1>
                    <div class="error">%s: %s</div>
                </body>
                </html>
            ]], error_msg.error, error_msg.details or "Unknown error"))
        end
        return
    end

    -- Get entries based on parameters
    local entries

    -- Filter entries based on query parameters
    if id then
        -- Get specific entry by ID
        local entry, get_err = snapshot:get(id)
        if not entry then
            res:set_status(http.STATUS.NOT_FOUND)
            if format == "json" then
                res:set_content_type(http.CONTENT.JSON)
                res:write_json({
                    error = "Entry not found",
                    id = id,
                    details = get_err
                })
            else
                res:set_content_type(http.CONTENT.TEXT)                    -- Use TEXT as base content type
                res:set_header("Content-Type", "text/html; charset=utf-8") -- Override with HTML
                res:write(string.format([[
                    <!DOCTYPE html>
                    <html>
                    <head>
                        <title>Entry Not Found</title>
                        <style>
                            body { font-family: Arial, sans-serif; margin: 2rem; }
                            .error { color: red; padding: 1rem; border: 1px solid red; background-color: #fff0f0; }
                        </style>
                    </head>
                    <body>
                        <h1>Entry Not Found</h1>
                        <div class="error">The entry with ID "%s" was not found: %s</div>
                    </body>
                    </html>
                ]], id, get_err or "Unknown error"))
            end
            return
        end
        entries = { entry }
    elseif namespace then
        -- Get entries by namespace
        entries = snapshot:namespace(namespace)
    else
        -- Get all entries
        entries = snapshot:entries()
    end

    -- Process entries into a clean format (without content data)
    local result = {}
    for i, entry in ipairs(entries) do
        -- Create simplified entry representation
        local idp = registry.parse_id(entry.id)

        local clean_entry = {
            id = {
                ns = idp.ns,
                name = idp.name,
                full = entry.id
            },
            kind = entry.kind,
            meta = entry.meta,
            has_data = entry.data ~= nil
        }

        -- Flag to track if the entry matches all search criteria
        local matches_search = true

        -- Check for specific metadata field matches
        if meta_name and #meta_name > 0 then
            local name_matches = false
            local meta_name_lower = meta_name:lower()

            if clean_entry.meta and clean_entry.meta.name then
                if type(clean_entry.meta.name) == "string" and
                    clean_entry.meta.name:lower():find(meta_name_lower, 1, true) then
                    name_matches = true
                end
            end

            if not name_matches then
                matches_search = false
            end
        end

        if meta_type and #meta_type > 0 and matches_search then
            local type_matches = false
            local meta_type_lower = meta_type:lower()

            if clean_entry.meta and clean_entry.meta.type then
                if type(clean_entry.meta.type) == "string" and
                    clean_entry.meta.type:lower():find(meta_type_lower, 1, true) then
                    type_matches = true
                end
            end

            if not type_matches then
                matches_search = false
            end
        end

        if meta_comment and #meta_comment > 0 and matches_search then
            local comment_matches = false
            local meta_comment_lower = meta_comment:lower()

            if clean_entry.meta and clean_entry.meta.comment then
                if type(clean_entry.meta.comment) == "string" and
                    clean_entry.meta.comment:lower():find(meta_comment_lower, 1, true) then
                    comment_matches = true
                end
            end

            if not comment_matches then
                matches_search = false
            end
        end

        -- Special handling for tag search (since tags are arrays)
        if meta_tag and #meta_tag > 0 and matches_search then
            local tag_matches = false
            local meta_tag_lower = meta_tag:lower()

            if clean_entry.meta and clean_entry.meta.tags then
                if type(clean_entry.meta.tags) == "table" then
                    -- Search through array of tags
                    for _, tag in ipairs(clean_entry.meta.tags) do
                        if type(tag) == "string" and tag:lower():find(meta_tag_lower, 1, true) then
                            tag_matches = true
                            break
                        end
                    end
                elseif type(clean_entry.meta.tags) == "string" and
                    clean_entry.meta.tags:lower():find(meta_tag_lower, 1, true) then
                    tag_matches = true
                end
            end

            if not tag_matches then
                matches_search = false
            end
        end

        -- Handle general search term if provided
        if search_term and #search_term > 0 and matches_search then
            local general_matches = false
            local search_term_lower = search_term:lower()

            -- Check if search term matches namespace or name
            if clean_entry.id.full:lower():find(search_term_lower, 1, true) or
                clean_entry.kind:lower():find(search_term_lower, 1, true) then
                general_matches = true
            end

            -- Check if search term matches any metadata value
            if not general_matches and clean_entry.meta then
                for k, v in pairs(clean_entry.meta) do
                    local meta_str = ""
                    if type(v) == "string" then
                        meta_str = v
                    elseif type(v) == "table" then
                        -- Handle array of values (like tags)
                        if #v > 0 then
                            for _, item in ipairs(v) do
                                if type(item) == "string" and item:lower():find(search_term_lower, 1, true) then
                                    general_matches = true
                                    break
                                end
                            end
                        else
                            -- Try to convert table metadata to string
                            local success, meta_json = pcall(json.encode, v)
                            if success and meta_json:lower():find(search_term_lower, 1, true) then
                                general_matches = true
                            end
                        end
                    elseif tostring(v):lower():find(search_term_lower, 1, true) then
                        general_matches = true
                    end

                    if k:lower():find(search_term_lower, 1, true) then
                        general_matches = true
                    end

                    if general_matches then
                        break
                    end
                end
            end

            if not general_matches then
                matches_search = false
            end
        end

        if matches_search then
            table.insert(result, clean_entry)
        end
    end

    -- Sort results by namespace, then by name
    table.sort(result, function(a, b)
        if a.id.ns == b.id.ns then
            return a.id.name < b.id.name
        else
            return a.id.ns < b.id.ns
        end
    end)

    -- Return based on requested format
    if format == "json" then
        -- Return JSON format
        res:set_content_type(http.CONTENT.JSON)
        res:set_status(http.STATUS.OK)
        res:write_json({
            count = #result,
            filter = {
                namespace = namespace,
                id = id,
                search = search_term,
                meta = {
                    name = meta_name,
                    type = meta_type,
                    tag = meta_tag,
                    comment = meta_comment
                }
            },
            entries = result
        })
    else
        -- Return HTML format - first set content type appropriately
        res:set_content_type(http.CONTENT.TEXT)                    -- Use TEXT as base content type
        res:set_header("Content-Type", "text/html; charset=utf-8") -- Override with HTML
        res:set_status(http.STATUS.OK)

        -- Start building HTML output
        local html = [[
<!DOCTYPE html>
<html>
<head>
    <title>Registry Explorer</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            line-height: 1.6;
            color: #333;
            margin: 0;
            padding: 20px;
        }

        h1, h2, h3 {
            margin-top: 0;
            color: #0366d6;
        }

        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 20px;
            padding-bottom: 10px;
            border-bottom: 1px solid #eaecef;
        }

        .search-form {
            background: #f6f8fa;
            padding: 15px;
            border-radius: 6px;
            margin-bottom: 20px;
        }

        .search-row {
            display: flex;
            flex-wrap: wrap;
            gap: 10px;
            margin-bottom: 10px;
        }

        .advanced-search {
            margin-top: 10px;
            border-top: 1px dashed #ddd;
            padding-top: 10px;
        }

        .advanced-search-toggle {
            background: none;
            border: none;
            color: #0366d6;
            cursor: pointer;
            padding: 5px 0;
            font-size: 14px;
            display: block;
            margin: 5px 0;
            text-align: left;
        }

        .search-form input, .search-form select {
            padding: 8px 12px;
            border: 1px solid #ddd;
            border-radius: 4px;
            font-size: 14px;
        }

        .search-form button[type="submit"] {
            background: #2ea44f;
            color: white;
            border: none;
            padding: 8px 16px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 14px;
        }

        .search-form button[type="submit"]:hover {
            background: #2c974b;
        }

        .reset-btn {
            background: #6c757d;
            color: white;
            border: none;
            padding: 8px 16px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 14px;
        }

        .reset-btn:hover {
            background: #5a6268;
        }

        .entry {
            margin-bottom: 25px;
            padding: 15px;
            border: 1px solid #eaecef;
            border-radius: 6px;
            background-color: #fff;
        }

        .entry:hover {
            border-color: #0366d6;
            box-shadow: 0 0 10px rgba(3, 102, 214, 0.1);
        }

        .entry-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 10px;
            padding-bottom: 8px;
            border-bottom: 1px dashed #eaecef;
        }

        .entry-title {
            font-weight: bold;
            font-size: 16px;
            margin: 0;
        }

        .entry-kind {
            display: inline-block;
            padding: 3px 10px;
            font-size: 12px;
            font-weight: 500;
            line-height: 1.5;
            color: #fff;
            background-color: #1f6feb;
            border-radius: 20px;
        }

        .entry-id {
            color: #586069;
            font-size: 14px;
            margin-bottom: 10px;
            font-family: SFMono-Regular, Consolas, "Liberation Mono", Menlo, monospace;
        }

        .entry-meta {
            background-color: #f6f8fa;
            border-radius: 4px;
            padding: 10px;
            font-size: 14px;
            overflow: auto;
        }

        .meta-title {
            margin: 5px 0;
            font-weight: bold;
            color: #24292e;
        }

        .meta-table {
            width: 100%;
            border-collapse: collapse;
        }

        .meta-table td, .meta-table th {
            padding: 5px;
            text-align: left;
            border-bottom: 1px solid #ddd;
        }

        .meta-table th {
            font-weight: 600;
            background-color: #f1f1f1;
        }

        .meta-key {
            font-weight: bold;
            width: 30%;
        }

        .json-link {
            color: #0366d6;
            text-decoration: none;
            font-size: 14px;
        }

        .json-link:hover {
            text-decoration: underline;
        }

        .count-info {
            font-size: 14px;
            color: #586069;
            margin-bottom: 15px;
        }

        .namespace-group {
            margin-bottom: 30px;
        }

        .namespace-title {
            font-size: 18px;
            padding: 5px 10px;
            background-color: #f1f8ff;
            border-radius: 4px;
            margin-bottom: 15px;
        }

        .active-filters {
            margin-bottom: 15px;
            font-size: 14px;
        }

        .filter-tag {
            display: inline-block;
            background: #e1ecf4;
            color: #39739d;
            padding: 2px 8px;
            margin-right: 5px;
            margin-bottom: 5px;
            border-radius: 3px;
        }

        .filter-tag a {
            color: #39739d;
            margin-left: 5px;
            text-decoration: none;
        }

        .filter-tag a:hover {
            color: #c13232;
        }

        /* Special styling for different kinds */
        .kind-http-endpoint .entry-kind {
            background-color: #2ea44f;
        }

        .kind-function-lua .entry-kind {
            background-color: #6f42c1;
        }

        .kind-library-lua .entry-kind {
            background-color: #e36209;
        }

        .kind-fs-directory .entry-kind {
            background-color: #0366d6;
        }

        .kind-http-service .entry-kind,
        .kind-http-router .entry-kind,
        .kind-http-static .entry-kind {
            background-color: #d73a49;
        }

        .kind-process-lua .entry-kind,
        .kind-process-service .entry-kind,
        .kind-process-host .entry-kind {
            background-color: #5a32a3;
        }
    </style>
    <script>
        function toggleAdvancedSearch() {
            const advancedSection = document.getElementById('advanced-search');
            if (advancedSection.style.display === 'none') {
                advancedSection.style.display = 'block';
                document.getElementById('toggle-btn').textContent = 'Hide Advanced Search';
            } else {
                advancedSection.style.display = 'none';
                document.getElementById('toggle-btn').textContent = 'Show Advanced Search';
            }
        }

        function clearSearch() {
            window.location.href = window.location.pathname;
        }

        function removeFilter(paramName) {
            const url = new URL(window.location.href);
            url.searchParams.delete(paramName);
            window.location.href = url.toString();
        }
    </script>
</head>
<body>
    <div class="header">
        <h1>Registry Explorer</h1>
        <a href="?format=json]] ..
            (search_term and ("&find=" .. search_term) or "") ..
            (namespace and ("&ns=" .. namespace) or "") ..
            (meta_name and ("&meta_name=" .. meta_name) or "") ..
            (meta_type and ("&meta_type=" .. meta_type) or "") ..
            (meta_tag and ("&meta_tag=" .. meta_tag) or "") ..
            (meta_comment and ("&meta_comment=" .. meta_comment) or "") ..
            [[" class="json-link">View as JSON</a>
    </div>

    <div class="search-form">
        <form action="" method="GET">
            <input type="hidden" name="format" value="html">

            <div class="search-row">
                <input type="text" name="find" placeholder="Search registry..." value="]] .. (search_term or "") .. [[">

                <select name="ns">
                    <option value="">All Namespaces</option>
        ]]

        -- Build list of unique namespaces for dropdown
        local namespaces = {}
        for _, entry in ipairs(entries) do
            local ipd = registry.parse_id(entry.id)

            if not namespaces[ipd.ns] then
                namespaces[ipd.ns] = true
            end
        end

        -- Sort namespaces
        local namespace_list = {}
        for ns in pairs(namespaces) do
            table.insert(namespace_list, ns)
        end
        table.sort(namespace_list)

        -- Add namespace options to the HTML
        for _, ns in ipairs(namespace_list) do
            local selected = namespace == ns and " selected" or ""
            html = html .. string.format(
                [[<option value="%s"%s>%s</option>]],
                ns, selected, ns
            )
        end

        -- Complete search form with advanced search options
        html = html .. [[
                </select>

                <button type="submit">Search</button>
                <button type="button" class="reset-btn" onclick="clearSearch()">Reset</button>
            </div>

            <button type="button" id="toggle-btn" class="advanced-search-toggle" onclick="toggleAdvancedSearch()">Show Advanced Search</button>

            <div id="advanced-search" class="advanced-search" style="display: none;">
                <div class="search-row">
                    <input type="text" name="meta_name" placeholder="Name..." value="]] .. (meta_name or "") .. [[">
                    <input type="text" name="meta_type" placeholder="Type..." value="]] .. (meta_type or "") .. [[">
                    <input type="text" name="meta_tag" placeholder="Tag..." value="]] .. (meta_tag or "") .. [[">
                    <input type="text" name="meta_comment" placeholder="Comment..." value="]] ..
        (meta_comment or "") .. [[">
                </div>
            </div>
        </form>
    </div>
    ]]

        -- Show active filters if any exist
        if search_term or namespace or meta_name or meta_type or meta_tag or meta_comment then
            html = html .. [[<div class="active-filters">Active filters: ]]

            if search_term and #search_term > 0 then
                html = html ..
                [[<span class="filter-tag">Search: ]] ..
                search_term .. [[<a href="javascript:removeFilter('find')">×</a></span>]]
            end

            if namespace then
                html = html ..
                [[<span class="filter-tag">Namespace: ]] ..
                namespace .. [[<a href="javascript:removeFilter('ns')">×</a></span>]]
            end

            if meta_name and #meta_name > 0 then
                html = html ..
                [[<span class="filter-tag">Name: ]] ..
                meta_name .. [[<a href="javascript:removeFilter('meta_name')">×</a></span>]]
            end

            if meta_type and #meta_type > 0 then
                html = html ..
                [[<span class="filter-tag">Type: ]] ..
                meta_type .. [[<a href="javascript:removeFilter('meta_type')">×</a></span>]]
            end

            if meta_tag and #meta_tag > 0 then
                html = html ..
                [[<span class="filter-tag">Tag: ]] ..
                meta_tag .. [[<a href="javascript:removeFilter('meta_tag')">×</a></span>]]
            end

            if meta_comment and #meta_comment > 0 then
                html = html ..
                [[<span class="filter-tag">Comment: ]] ..
                meta_comment .. [[<a href="javascript:removeFilter('meta_comment')">×</a></span>]]
            end

            html = html .. [[</div>]]
        end

        html = html .. [[
    <div class="count-info">
        Found <strong>]] .. #result .. [[</strong> entries
    </div>
        ]]

        -- Group entries by namespace
        local grouped_entries = {}
        for _, entry in ipairs(result) do
            local ns = entry.id.ns
            if not grouped_entries[ns] then
                grouped_entries[ns] = {}
            end
            table.insert(grouped_entries[ns], entry)
        end

        -- Sort namespaces
        local sorted_namespaces = {}
        for ns in pairs(grouped_entries) do
            table.insert(sorted_namespaces, ns)
        end
        table.sort(sorted_namespaces)

        -- Generate HTML for each namespace group
        for _, ns in ipairs(sorted_namespaces) do
            local ns_entries = grouped_entries[ns]

            -- Namespace header
            html = html .. string.format([[
            <div class="namespace-group">
                <div class="namespace-title">%s</div>
            ]], ns)

            -- Generate HTML for each entry in this namespace
            for _, entry in ipairs(ns_entries) do
                -- Convert kind to css-friendly format
                local kind_class = "kind-" .. entry.kind:gsub("%.", "-")

                -- Start entry div
                html = html .. string.format([[
                <div class="entry %s">
                    <div class="entry-header">
                        <h3 class="entry-title">%s</h3>
                        <span class="entry-kind">%s</span>
                    </div>
                    <div class="entry-id">%s</div>
                ]],
                    kind_class,
                    entry.id.name,
                    entry.kind,
                    entry.id.full
                )

                -- Add metadata section if present
                if entry.meta and next(entry.meta) then
                    html = html .. [[
                    <div class="meta-title">Metadata</div>
                    <div class="entry-meta">
                        <table class="meta-table">
                    ]]

                    -- Generate rows for each metadata key/value
                    for k, v in pairs(entry.meta) do
                        local value = ""

                        -- Format value based on type
                        if type(v) == "table" then
                            -- For tag arrays, add clickable links to search for each tag
                            if k == "tags" then
                                local items = {}
                                for i, tag in ipairs(v) do
                                    local tag_link = string.format(
                                        [[<a href="?meta_tag=%s">%s</a>]],
                                        tag, tag
                                    )
                                    table.insert(items, tag_link)
                                end
                                value = table.concat(items, ", ")
                                -- For other arrays
                            elseif #v > 0 then -- array-like table
                                local items = {}
                                for i, item in ipairs(v) do
                                    table.insert(items, tostring(item))
                                end
                                value = table.concat(items, ", ")
                            else -- object-like table
                                local parts = {}
                                for key, val in pairs(v) do
                                    table.insert(parts, key .. ": " .. tostring(val))
                                end
                                value = table.concat(parts, ", ")
                            end
                        else
                            -- For key-specific formatting and search links
                            if k == "name" or k == "type" or k == "comment" then
                                value = string.format(
                                    [[%s <a href="?meta_%s=%s" class="json-link">(search)</a>]],
                                    tostring(v), k, tostring(v)
                                )
                            else
                                value = tostring(v)
                            end
                        end

                        -- Add table row for this metadata
                        html = html .. string.format([[
                        <tr>
                            <td class="meta-key">%s</td>
                            <td>%s</td>
                        </tr>
                        ]], k, value)
                    end

                    html = html .. [[
                        </table>
                    </div>
                    ]]
                end

                -- Add link to view full entry
                html = html .. string.format([[
                    <p><a href="?id=%s&format=json" class="json-link">View Full Entry</a></p>
                </div>
                ]], entry.id.full)
            end

            -- Close namespace group
            html = html .. [[
            </div>
            ]]
        end

        -- Complete HTML
        html = html .. [[
</body>
</html>
        ]]

        -- Send HTML response
        res:write(html)
    end
end

-- Export the function
return {
    handler = handler
}
