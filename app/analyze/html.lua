local M = {}

-- Helper to determine if we should preserve whitespace
local function should_preserve_whitespace(node)
    -- Check if node is inside a <pre> tag
    local current = node
    while current do
        if current:type() == "element" then
            local tag = current:child(0) -- start_tag
            if tag and tag:child(0) and tag:child(0):type() == "tag_name" then
                local tag_name = tag:child(0):text()
                if tag_name == "pre" then
                    return true
                end
            end
        end
        current = current:parent()
    end
    return false
end

-- Format text based on context
local function format_text(text, preserve_whitespace)
    if preserve_whitespace then
        return text
    end
    -- Normalize whitespace but preserve intentional line breaks
    return text:gsub("%s+", " "):gsub("^%s+", ""):gsub("%s+$", "")
end

-- Helper to determine if node indicates a line break
local function is_line_break(node)
    if node:type() == "element" then
        local tag = node:child(0) -- start_tag
        if tag and tag:child(0) and tag:child(0):type() == "tag_name" then
            local tag_name = tag:child(0):text()
            return tag_name == "br" or tag_name == "p" or tag_name == "div" or
                   tag_name == "h1" or tag_name == "h2" or tag_name == "h3" or
                   tag_name == "h4" or tag_name == "h5" or tag_name == "h6" or
                   tag_name == "li" or tag_name == "tr"
        end
    end
    return false
end

-- Function to extract links from HTML
function M.extract_links(filepath)
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

    local parser = treesitter.parser()
    if not parser then
        return nil, "Failed to create parser"
    end

    local ok, err = pcall(function()
        parser:set_language("html")
    end)
    if not ok then
        return nil, "Failed to set HTML language: " .. tostring(err)
    end

    local tree = parser:parse(content)
    if not tree then
        return nil, "Failed to parse file"
    end

    local root = tree:root_node()
    local links = {}

    -- Query for links with their attributes and text content
    local link_query = treesitter.query("html", [[
        (element
            (start_tag
                (tag_name) @tag
                (#eq? @tag "a")
                (attribute
                    (attribute_name) @attr.name
                    (quoted_attribute_value) @attr.value)*) @start
            (text)? @link.text) @link
    ]])

    if link_query then
        for _, match in ipairs(link_query:matches(root, content)) do
            local link_info = {
                href = "",
                text = "",
                title = "",
                rel = "",
                target = ""
            }

            -- Get the href and other attributes
            local start_tag = match.captures[1].node:parent()
            if start_tag then
                for i = 0, start_tag:named_child_count() - 1 do
                    local attr = start_tag:named_child(i)
                    if attr:type() == "attribute" then
                        local name_node = attr:child(0)
                        local value_node = attr:child(1)
                        if name_node and value_node then
                            local name = name_node:text(content)
                            local value = value_node:text(content):gsub('"', '')

                            if name == "href" then
                                link_info.href = value
                            elseif name == "title" then
                                link_info.title = value
                            elseif name == "rel" then
                                link_info.rel = value
                            elseif name == "target" then
                                link_info.target = value
                            end
                        end
                    end
                end
            end

            -- Get the link text
            local text = ""
            local link_element = match.captures[#match.captures].node
            for i = 0, link_element:named_child_count() - 1 do
                local child = link_element:named_child(i)
                if child:type() == "text" then
                    text = text .. child:text(content)
                end
            end

            link_info.text = format_text(text, false)

            -- Only include links with href
            if link_info.href ~= "" then
                table.insert(links, link_info)
            end
        end
    end

    -- Clean up
    if parser then parser:close() end
    if tree then tree:close() end

    return links
end

-- Main extraction function
function M.extract_content(filepath)
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

    -- Storage for extracted text
    local text_parts = {}
    local prev_was_text = false

    -- Query for text nodes
    local text_query = treesitter.query("html", [[
        (text) @text
        (raw_text) @raw_text
    ]])

    if text_query then
        for _, match in ipairs(text_query:matches(root, content)) do
            local node = match.captures[1].node
            local text = node:text(content)

            -- Skip if parent is script or style
            local parent = node:parent()
            if parent and (parent:type() == "script_element" or parent:type() == "style_element") then
                goto continue
            end

            -- Check if we need to preserve whitespace
            local preserve = should_preserve_whitespace(node)

            -- Format the text appropriately
            text = format_text(text, preserve)

            -- Skip if text is empty after formatting
            if text == "" then
                goto continue
            end

            -- Add line break if needed
            if prev_was_text and is_line_break(node:parent()) then
                table.insert(text_parts, "\n")
            end

            -- Add the text
            table.insert(text_parts, text)
            prev_was_text = true

            ::continue::
        end
    end

    -- Clean up
    if parser then parser:close() end
    if tree then tree:close() end

    -- Join all parts with proper spacing
    local final_text = table.concat(text_parts, " ")

    -- Final cleanup
    final_text = final_text:gsub("%s*\n%s*", "\n")  -- Clean up around newlines
                         :gsub("\n\n+", "\n\n")      -- Maximum double newlines
                         :gsub("%s+", " ")           -- No multiple spaces
                         :gsub("^%s+", "")           -- No leading whitespace
                         :gsub("%s+$", "")           -- No trailing whitespace

    return {
        text = final_text
    }
end

-- Main analyze function that uses both extractors
function M.analyze(filepath)
    local content_result = M.extract_content(filepath)
    if not content_result then
        return nil, "Failed to extract content"
    end

    local links = M.extract_links(filepath)
    if not links then
        return nil, "Failed to extract links"
    end

    local text = content_result.text
    local lines = 0
    local words = 0
    local chars = #text

    -- Count lines and words
    for line in text:gmatch("[^\n]+") do
        lines = lines + 1
        for word in line:gmatch("%S+") do
            words = words + 1
        end
    end

    -- Categorize links
    local internal_links = 0
    local external_links = 0
    local anchor_links = 0

    for _, link in ipairs(links) do
        if link.href:match("^#") then
            anchor_links = anchor_links + 1
        elseif link.href:match("^https?://") then
            external_links = external_links + 1
        else
            internal_links = internal_links + 1
        end
    end

    -- Generate report
    local report = string.format([[
Content and Link Analysis Report
-------------------------------
Content Statistics:
  Lines: %d
  Words: %d
  Characters: %d

Link Statistics:
  Total Links: %d
  - External Links: %d
  - Internal Links: %d
  - Anchor Links: %d

Links Found:
%s

Extracted Content:
----------------------------------------
%s
----------------------------------------
]],
        lines, words, chars,
        #links, external_links, internal_links, anchor_links,
        table.concat(
            (function()
                local link_texts = {}
                for i, link in ipairs(links) do
                    local link_info = string.format(
                        "  %d. %s\n     URL: %s%s%s%s",
                        i,
                        link.text ~= "" and link.text or "[No text]",
                        link.href,
                        link.title ~= "" and "\n     Title: " .. link.title or "",
                        link.rel ~= "" and "\n     Rel: " .. link.rel or "",
                        link.target ~= "" and "\n     Target: " .. link.target or ""
                    )
                    table.insert(link_texts, link_info)
                end
                return link_texts
            end)(),
            "\n\n"
        ),
        text
    )

    return {
        text = report,
        data = {
            content = text,
            links = links,
            metrics = {
                lines = lines,
                words = words,
                chars = chars,
                total_links = #links,
                external_links = external_links,
                internal_links = internal_links,
                anchor_links = anchor_links
            }
        }
    }
end

return M