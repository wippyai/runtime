local http = require("http")
local json = require("json")
local fs = require("fs")
local http_client = require("http_client")

-- Utility function to split a string by newline
local function split_by_newline(str)
    local result = {}
    for line in string.gmatch(str, "([^\n]*)\n?") do
        table.insert(result, line)
    end
    return result
end

-- Function to determine heading level from a line
local function get_heading_level(line)
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
local function extract_heading_text(line)
    return string.match(line, "^#+%s+(.+)$") or ""
end

-- Find a good boundary for splitting text, prioritizing line breaks
local function find_chunk_boundary(text, target_position)
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
local function chunk_markdown(md_content, options)
    options = options or {}
    local min_chunk_size = options.min_chunk_size or 100
    local max_chunk_size = options.max_chunk_size or 1000
    local overlap_size = options.overlap_size or 50

    local lines = split_by_newline(md_content)
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
        local heading_level = get_heading_level(line)

        if heading_level > 0 then
            -- This is a heading line, handle it
            local heading_text = extract_heading_text(line)

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
            current_chunk = {
                content = line .. "\n",
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
                local split_position = find_chunk_boundary(current_chunk.content, max_chunk_size)

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

local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response context"
    end

    -- Get file from public filesystem
    local public_fs = fs.get("app:public")
    if not public_fs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to access public filesystem"
        })
        return
    end

    -- Check if file exists
    if not public_fs:exists("sample.pdf") then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write_json({
            success = false,
            error = "File sample.pdf not found in public directory"
        })
        return
    end

    -- Open the file
    local file, err = public_fs:open("sample.docx", "r")
    if not file then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to open file: " .. (err or "unknown error")
        })
        return
    end

    -- Make request to local Dockling API - get structured data with page breaks
    local response, err = http_client.post("http://localhost:5001/v1alpha/convert/file", {
        timeout = "3m", -- 3 minute timeout as requested
        files = {
            {
                name = "files",
                filename = "sample.pdf",
                content_type = "application/pdf",
                reader = file
            }
        },
        form = "do_ocr=false&return_as_file=false&to_formats=md&pdf_backend=pypdfium2"
    })

    -- Close the file
    file:close()

    -- Handle request error
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Dockling API request failed: " .. err
        })
        return
    end

    -- Handle non-200 response
    if response.status_code ~= 200 then
        res:set_status(http.STATUS.BAD_GATEWAY)
        res:write_json({
            success = false,
            error = "Dockling API returned non-200 status: " .. response.status_code,
            dockling_response = response.body
        })
        return
    end

    -- Parse the JSON response from Dockling
    local dockling_data = json.decode(response.body)

    -- Get the markdown content from the response
    local md_content = dockling_data.document.md_content

    -- If we have Markdown content, perform the smart chunking
    local result = dockling_data
    if md_content then
        -- Configure chunking options
        local chunk_options = {
            min_chunk_size = 100,
            max_chunk_size = 1000,
            overlap_size = 50
        }

        -- Perform the smart chunking
        local chunks = chunk_markdown(md_content, chunk_options)

        -- Add chunks to the result
        result.chunks = chunks
    end

    -- Return the response with chunks
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write(json.encode(result))
end

return {
    handler = handler
}