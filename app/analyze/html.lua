local M = {}

-- Helper function for safe text extraction
local function safe_text(str)
    return str or "unknown"
end

-- Helper function for safe list formatting
local function format_list(items, formatter)
    local result = {}
    for _, item in ipairs(items or {}) do
        local formatted = formatter(item)
        if formatted then
            table.insert(result, formatted)
        end
    end
    return table.concat(result, "\n")
end

function M.analyze(filepath)
    -- Get required modules
    local fs = require("fs")
    local treesitter = require("treesitter")
    local myfs = fs.get("system:core")

    if not myfs then
        return nil, "Failed to get filesystem"
    end

    local content = myfs:readfile(filepath)
    if not content then
        return nil, "Failed to read file"
    end

    print("Debug: Creating parser...")
    local parser = treesitter.parser()
    if not parser then
        return nil, "Failed to create parser"
    end

    print("Debug: Setting language to HTML...")
    local ok, err = pcall(function()
        parser:set_language("html")
    end)
    if not ok then
        print("Error setting language:", err)
        return nil, "Failed to set HTML language: " .. tostring(err)
    end

    print("Debug: Parsing HTML file...")
    local tree = parser:parse(content)
    if not tree then
        return nil, "Failed to parse file"
    end

    local root = tree:root_node()
    if root:has_error() then
        print("Warning: Syntax errors found in file")
    end

    -- Initialize analysis structure
    local analysis = {
        doctype = "",
        head = {
            title = "",
            meta = {},
            links = {},
            scripts = {},
            styles = {}
        },
        body = {
            elements = {},
            forms = {},
            scripts = {}
        },
        metrics = {
            total_elements = 0,
            total_attributes = 0,
            total_forms = 0,
            total_scripts = 0,
            total_styles = 0,
            total_comments = 0,
            nesting_depth = 0
        }
    }

    print("Debug: Creating queries...")

    -- Create query for DOCTYPE
    local doctype_query = treesitter.query("html", [[
        (doctype) @doctype
    ]])

    -- Query for head elements
    local head_query = treesitter.query("html", [[
        (element
            (start_tag
                (tag_name) @tag.name
                (#eq? @tag.name "head"))
            (element) @head.content)
    ]])

    -- Query for title, meta, link
    local meta_query = treesitter.query("html", [[
        (element
            (start_tag
                (tag_name) @tag.name
                (#match? @tag.name "^(title|meta|link)$")
                (attribute
                    (attribute_name) @attr.name
                    (quoted_attribute_value
                        (attribute_value) @attr.value))*)) @element
    ]])

    -- Query for scripts
    local script_query = treesitter.query("html", [[
        (script_element
            (start_tag
                (tag_name) @tag.name
                (attribute
                    (attribute_name) @attr.name
                    (quoted_attribute_value
                        (attribute_value) @attr.value))*)
            (raw_text)? @script.content) @script
    ]])

    -- Query for styles
    local style_query = treesitter.query("html", [[
        (style_element
            (start_tag
                (tag_name) @tag.name)
            (raw_text) @style.content) @style
    ]])

    -- Query for regular elements
    local element_query = treesitter.query("html", [[
        (element
            (start_tag
                (tag_name) @tag.name
                (attribute
                    (attribute_name) @attr.name
                    (quoted_attribute_value
                        (attribute_value) @attr.value))*)) @element
    ]])

    -- Query for comments
    local comment_query = treesitter.query("html", [[
        (comment) @comment
    ]])

    -- Process DOCTYPE
    if doctype_query then
        for _, match in ipairs(doctype_query:matches(root, content)) do
            analysis.doctype = match.captures[1].node:text(content)
        end
    end

    -- Helper function to process attributes
    local function get_attributes(node)
        local attrs = {}
        local attr_query = treesitter.query("html", [[
            (attribute
                (attribute_name) @attr.name
                (quoted_attribute_value
                    (attribute_value) @attr.value))
        ]])

        if attr_query then
            for _, match in ipairs(attr_query:matches(node, content)) do
                local name = match.captures[1].node:text(content)
                local value = match.captures[2].node:text(content)
                attrs[name] = value
            end
        end
        return attrs
    end

    -- Process head elements
    if head_query then
        for _, match in ipairs(head_query:matches(root, content)) do
            local head_content = match.captures[2].node

            -- Process meta elements
            if meta_query then
                for _, meta_match in ipairs(meta_query:matches(head_content, content)) do
                    local tag_name = meta_match.captures[1].node:text(content)
                    local element_node = meta_match.captures[#meta_match.captures].node
                    local attrs = get_attributes(element_node)

                    if tag_name == "title" then
                        -- Get text content for title
                        for child in element_node:iter_children() do
                            if child:type() == "text" then
                                analysis.head.title = child:text(content)
                                break
                            end
                        end
                    elseif tag_name == "meta" then
                        table.insert(analysis.head.meta, attrs)
                    elseif tag_name == "link" then
                        table.insert(analysis.head.links, attrs)
                    end
                end
            end

            -- Process scripts
            if script_query then
                for _, script_match in ipairs(script_query:matches(head_content, content)) do
                    local attrs = get_attributes(script_match.captures[1].node)
                    if script_match.captures[2] then
                        attrs.content = script_match.captures[2].node:text(content)
                    end
                    table.insert(analysis.head.scripts, attrs)
                    analysis.metrics.total_scripts = analysis.metrics.total_scripts + 1
                end
            end

            -- Process styles
            if style_query then
                for _, style_match in ipairs(style_query:matches(head_content, content)) do
                    local style_content = style_match.captures[2].node:text(content)
                    table.insert(analysis.head.styles, style_content)
                    analysis.metrics.total_styles = analysis.metrics.total_styles + 1
                end
            end
        end
    end

    -- Helper function to calculate nesting depth
    local function get_nesting_depth(node, current_depth)
        local max_depth = current_depth

        for i = 0, node:named_child_count() - 1 do
            local child = node:named_child(i)
            if child:type() == "element" then
                local child_depth = get_nesting_depth(child, current_depth + 1)
                max_depth = math.max(max_depth, child_depth)
            end
        end

        return max_depth
    end

    -- Process comments
    if comment_query then
        for _, match in ipairs(comment_query:matches(root, content)) do
            analysis.metrics.total_comments = analysis.metrics.total_comments + 1
        end
    end

    -- Calculate nesting depth
    analysis.metrics.nesting_depth = get_nesting_depth(root, 0)

    -- Count total elements
    if element_query then
        for _ in ipairs(element_query:matches(root, content)) do
            analysis.metrics.total_elements = analysis.metrics.total_elements + 1
        end
    end

    -- Generate report
    local report = string.format([[
HTML Analysis Report (Tree-sitter Enhanced)
----------------------------------------
Document Type: %s

Head Section:
  Title: %s
  Meta Tags: %d
  Link Tags: %d
  Scripts: %d
  Styles: %d

Metrics:
  Total Elements: %d
  Total Scripts: %d
  Total Styles: %d
  Total Comments: %d
  Maximum Nesting Depth: %d

Meta Tags:
%s

Link Tags:
%s

External Scripts:
%s

Styles:
%s
]],
        safe_text(analysis.doctype),
        safe_text(analysis.head.title),
        #analysis.head.meta,
        #analysis.head.links,
        analysis.metrics.total_scripts,
        analysis.metrics.total_styles,

        analysis.metrics.total_elements,
        analysis.metrics.total_scripts,
        analysis.metrics.total_styles,
        analysis.metrics.total_comments,
        analysis.metrics.nesting_depth,

        format_list(analysis.head.meta, function(meta)
            local attrs = {}
            for name, value in pairs(meta) do
                table.insert(attrs, string.format("%s=\"%s\"", name, value))
            end
            return "  <meta " .. table.concat(attrs, " ") .. ">"
        end),

        format_list(analysis.head.links, function(link)
            local attrs = {}
            for name, value in pairs(link) do
                table.insert(attrs, string.format("%s=\"%s\"", name, value))
            end
            return "  <link " .. table.concat(attrs, " ") .. ">"
        end),

        format_list(analysis.head.scripts, function(script)
            if script.src then
                return "  " .. script.src
            end
            return "  [Inline script]"
        end),

        format_list(analysis.head.styles, function(style)
            local preview = style:gsub("^%s+", ""):gsub("%s+$", "")
            if #preview > 60 then
                preview = preview:sub(1, 57) .. "..."
            end
            return "  " .. preview
        end)
    )

    -- Clean up
    if parser then parser:close() end
    if tree then tree:close() end

    return {
        text = report
    }
end

return M