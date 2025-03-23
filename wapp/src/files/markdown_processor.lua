local json = require("json")

-- Maximum length for section titles in database
local MAX_TITLE_LENGTH = 255

local markdown_processor = {}

-- Helper function to split a string by newline
function markdown_processor.split_by_newline(str)
    local result = {}
    for line in string.gmatch(str, "([^\n]*)\n?") do
        table.insert(result, line)
    end
    return result
end

-- Function to determine heading level from a line
function markdown_processor.get_heading_level(line)
    local level = 0
    for i = 1, 6 do
        if string.match(line, "^" .. string.rep("#", i) .. "%s+") then
            level = i
            break
        end
    end
    return level
end

-- Extract heading text from a heading line
function markdown_processor.extract_heading_text(line)
    return string.match(line, "^#+%s+(.+)$") or ""
end

-- Function to safely truncate title to maximum length at a reasonable boundary
function markdown_processor.truncate_title(title, max_length)
    if not max_length then
        max_length = MAX_TITLE_LENGTH - 10 -- Leave some buffer
    end

    if #title <= max_length then
        return title, "" -- Title is already short enough
    end

    -- Define break points in order of preference
    local break_points = {
        ". ", "! ", "? ", -- End of sentence
        ": ", "; ",       -- Clause separators
        ", ",             -- Commas
        " "               -- Last resort: any space
    }

    -- Find the last occurrence of each break point within max_length
    local best_pos = nil
    for _, point in ipairs(break_points) do
        local pos = 0
        local last_pos = 0

        while true do
            pos = string.find(title, point, pos + 1)
            if not pos or pos > max_length then break end
            last_pos = pos
        end

        if last_pos > 0 then
            best_pos = last_pos + #point - 1 -- Include the punctuation in first part
            break
        end
    end

    -- If no good break point found, just cut at max_length
    if not best_pos then
        best_pos = max_length
    end

    -- Split the title
    local first_part = string.sub(title, 1, best_pos)
    local remaining = string.sub(title, best_pos + 1)

    -- Trim any leading whitespace from remaining part
    remaining = string.match(remaining or "", "^%s*(.-)$")

    return first_part, remaining
end

-- Find a good boundary for splitting text, prioritizing line breaks
function markdown_processor.find_chunk_boundary(text, target_position)
    -- Define boundary types in priority order
    local boundaries = {
        -- Highest priority: double newline (paragraph breaks)
        {pattern = "\n\n", search_range = 300, min_distance = 20},
        -- High priority: single newline (line breaks)
        {pattern = "\n", search_range = 250, min_distance = 20},
        -- Medium priority: punctuation that ends sentences
        {pattern = "%.%s", search_range = 200, min_distance = 30},
        {pattern = "!%s", search_range = 200, min_distance = 30},
        {pattern = "?%s", search_range = 200, min_distance = 30},
        -- Lower priority: punctuation that separates clauses
        {pattern = ";%s", search_range = 150, min_distance = 30},
        {pattern = ":%s", search_range = 150, min_distance = 30},
        -- Lowest priority: other common separators
        {pattern = ",%s", search_range = 100, min_distance = 40}
    }

    -- If target position is at the end, return it
    if target_position >= #text then
        return #text
    end

    -- Try each boundary type in order of priority
    for _, boundary in ipairs(boundaries) do
        local search_end = math.min(target_position + boundary.search_range, #text)

        -- Look ahead from the target position for this boundary type
        local pos = target_position
        while pos < search_end do
            pos = string.find(text, boundary.pattern, pos + 1)
            if not pos then break end

            -- If we found a boundary at an acceptable distance, use it
            local distance = pos - target_position
            if distance >= boundary.min_distance then
                return pos
            end
        end
    end

    -- If no satisfactory boundary was found, just use the target position
    return target_position
end

-- Smart markdown chunking function
function markdown_processor.chunk_markdown(md_content, options)
    options = options or {}
    local min_chunk_size = options.min_chunk_size or 100
    local max_chunk_size = options.max_chunk_size or 1000
    local overlap_size = options.overlap_size or 50

    local lines = markdown_processor.split_by_newline(md_content)
    local chunks = {}
    local hierarchy = {}
    local current_chunk = {
        content = "",
        metadata = {
            heading_path = {},
            current_section = "",
            title = "Document Root",
            chunk_id = 1
        }
    }

    local heading_stack = {}
    local chunk_counter = 1

    for i, line in ipairs(lines) do
        local heading_level = markdown_processor.get_heading_level(line)

        if heading_level > 0 then
            -- This is a heading line, handle it
            local heading_text = markdown_processor.extract_heading_text(line)

            -- Handle long section titles
            local title_first_part, title_remaining = markdown_processor.truncate_title(heading_text)
            heading_text = title_first_part

            -- If we have content in the current chunk, save it before starting a new section
            if #current_chunk.content > min_chunk_size then
                table.insert(chunks, current_chunk)
                chunk_counter = chunk_counter + 1
            end

            -- Update heading stack
            -- Pop any headings that are same or higher level than current
            while #heading_stack > 0 and heading_stack[#heading_stack].level >= heading_level do
                table.remove(heading_stack)
            end

            -- Add current heading to stack
            local heading_item = {
                level = heading_level,
                text = heading_text,
                chunks = {},
                children = {}
            }

            table.insert(heading_stack, heading_item)

            -- Update hierarchy
            if #heading_stack == 1 then
                -- Top level heading
                table.insert(hierarchy, heading_item)
            elseif #heading_stack > 1 then
                -- Child heading
                local parent = heading_stack[#heading_stack - 1]
                table.insert(parent.children, heading_item)
            end

            -- Create new chunk with the heading
            local chunk_content = line .. "\n"

            -- Add remaining part of title to the chunk content if there was any
            if title_remaining and title_remaining ~= "" then
                chunk_content = chunk_content .. title_remaining .. "\n"
            end

            current_chunk = {
                content = chunk_content,
                metadata = {
                    heading_path = {},
                    current_section = heading_text,
                    title = heading_text,
                    chunk_id = chunk_counter
                }
            }

            -- Update metadata heading path
            for _, h in ipairs(heading_stack) do
                table.insert(current_chunk.metadata.heading_path, h.text)
                -- Track which chunks belong to this heading
                table.insert(h.chunks, chunk_counter)
            end
        else
            -- Add line to current chunk
            current_chunk.content = current_chunk.content .. line .. "\n"

            -- Check if we need to split due to size
            if #current_chunk.content >= max_chunk_size then
                -- Find a good split point near max_chunk_size, prioritizing line breaks
                local split_position = markdown_processor.find_chunk_boundary(current_chunk.content, max_chunk_size)

                -- Split at this position
                local first_part = string.sub(current_chunk.content, 1, split_position)
                local second_part = string.sub(current_chunk.content, split_position + 1)

                -- Update current chunk content
                current_chunk.content = first_part
                table.insert(chunks, current_chunk)

                -- Create next chunk
                chunk_counter = chunk_counter + 1
                current_chunk = {
                    content = second_part,
                    metadata = {
                        heading_path = {},
                        current_section = current_chunk.metadata.current_section,
                        title = current_chunk.metadata.title,
                        is_continuation = true,
                        chunk_id = chunk_counter
                    }
                }

                -- Copy heading path and update chunk references
                for _, h in ipairs(heading_stack) do
                    table.insert(current_chunk.metadata.heading_path, h.text)
                    table.insert(h.chunks, chunk_counter)
                end
            end
        end
    end

    -- Add the last chunk if it has content
    if #current_chunk.content > 0 then
        table.insert(chunks, current_chunk)
    end

    return chunks, hierarchy
end

-- Function to extract main sections of the document
function markdown_processor.extract_sections(hierarchy)
    local sections = {}

    local function process_section(section, depth, parent_path)
        local current_path = parent_path and (parent_path .. " > " .. section.text) or section.text

        -- Add this section
        table.insert(sections, {
            title = section.text,
            level = section.level,
            path = current_path,
            chunks = #section.chunks
        })

        -- Process children recursively
        for _, child in ipairs(section.children or {}) do
            process_section(child, depth + 1, current_path)
        end
    end

    -- Process top-level sections
    for _, section in ipairs(hierarchy) do
        process_section(section, 1, nil)
    end

    return sections
end

-- Check if a document appears to be in markdown format
function markdown_processor.is_markdown(content)
    if not content or type(content) ~= "string" then
        return false
    end

    -- Look for common markdown features
    local markdown_patterns = {
        "^#%s+", -- Heading
        "%*%*.+%*%*", -- Bold
        "_%_.+_%_", -- Italic
        "%*%s+", -- Unordered list
        "%d+%.%s+", -- Ordered list
        "%[.-%]%(.-%)\"%%" -- Links
    }

    for _, pattern in ipairs(markdown_patterns) do
        if string.match(content, pattern) then
            return true
        end
    end

    -- Check for code blocks
    if string.match(content, "```") then
        return true
    end

    return false
end

-- Analyze the document to determine its properties
function markdown_processor.analyze_document(content)
    if not content or type(content) ~= "string" then
        return nil, "Invalid content"
    end

    local stats = {
        total_length = #content,
        line_count = 0,
        heading_count = 0,
        code_blocks = 0,
        lists = 0,
        estimated_reading_time_minutes = 0
    }

    -- Count lines
    local lines = markdown_processor.split_by_newline(content)
    stats.line_count = #lines

    -- Count headings and code blocks
    local in_code_block = false
    local list_markers = 0

    for _, line in ipairs(lines) do
        -- Check for headings
        if markdown_processor.get_heading_level(line) > 0 then
            stats.heading_count = stats.heading_count + 1
        end

        -- Check for code blocks
        if string.match(line, "^```") then
            in_code_block = not in_code_block
            if in_code_block then
                stats.code_blocks = stats.code_blocks + 1
            end
        end

        -- Check for list items
        if string.match(line, "^%s*[-*+]%s") or string.match(line, "^%s*%d+%.%s") then
            list_markers = list_markers + 1
        end
    end

    -- Estimate lists (consecutive list markers)
    stats.lists = math.ceil(list_markers / 3) -- Rough estimate assuming avg 3 items per list

    -- Calculate estimated reading time (average reading speed: 200 words per minute)
    local word_count = 0
    for word in string.gmatch(content, "%S+") do
        word_count = word_count + 1
    end
    stats.word_count = word_count
    stats.estimated_reading_time_minutes = math.ceil(word_count / 200)

    return stats
end

return markdown_processor