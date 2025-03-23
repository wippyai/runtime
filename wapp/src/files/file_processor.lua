local fs = require("fs")
local llm = require("llm")
local json = require("json")
local time = require("time")
local http_client = require("http_client")

local file_repo = require("file_repo")
local markdown_processor = require("markdown_processor")

local file_processor = {}

-- Generate embedding for text
local function generate_embedding(text)
    -- Use text-embedding-3-small model for embeddings
    local response = llm.embed(text, {
        model = "text-embedding-3-small",
        dimensions = 512
    })

    if not response or response.error then
        return nil, "Failed to generate embedding: " .. (response and response.error_message or "Unknown error")
    end

    -- Return embedding array
    return "[" .. table.concat(response.result, ",") .. "]"
end

-- Convert document to markdown using Dockling API
local function convert_to_markdown(file_path, mime_type)
    -- Open the file
    local uploads_fs = fs.get("app:uploads")
    if not uploads_fs then
        return nil, "Failed to access uploads filesystem"
    end

    local file, err = uploads_fs:open(file_path, "r")
    if not file then
        return nil, "Failed to open file: " .. (err or "unknown error")
    end

    print(string.format("Converting file: %s (type: %s)", file_path, mime_type))

    -- Setup OCR option based on file type
    local do_ocr = "false"
    if string.match(mime_type, "image/") or mime_type == "application/pdf" then
        do_ocr = "true" -- Enable OCR for image files and PDFs
    end

    -- Extract filename from path
    local filename = file_path:match("([^/]+)$") or "document"

    -- Make request to local Dockling API
    local response, err = http_client.post("http://localhost:5001/v1alpha/convert/file", {
        timeout = "5m", -- 5 minute timeout for large files
        files = {
            {
                name = "files",
                filename = filename,
                content_type = mime_type,
                reader = file
            }
        },
        form = "do_ocr=" .. do_ocr .. "&return_as_file=false&to_formats=md&pdf_backend=pypdfium2"
    })

    -- Close file handle
    file:close()

    if err then
        return nil, "Dockling API request failed: " .. err
    end

    -- Handle non-200 response
    if response.status_code ~= 200 then
        return nil, "Dockling API returned error: " .. response.status_code .. " - " .. response.body
    end

    -- Parse response JSON
    local dockling_data = json.decode(response.body)

    -- Extract markdown content
    local md_content = dockling_data.document.md_content
    if not md_content or md_content == "" then
        return nil, "No markdown content received from conversion service"
    end

    print(string.format("Successfully converted file to markdown: %d bytes", #md_content))

    return md_content
end

-- Process a file
function file_processor.process(file_id)
    -- Get file info
    local file, err = file_repo.get(file_id)
    if err then
        return nil, "Failed to get file: " .. err
    end

    -- Update file status to processing
    local status, err = file_repo.update_status(file_id, "processing")
    if err then
        return nil, "Failed to update file status: " .. err
    end

    -- Get upload filesystem
    local uploads_fs = fs.get("app:uploads")
    if not uploads_fs then
        file_repo.update_status(file_id, "error")
        return nil, "Failed to access uploads filesystem"
    end

    -- Check if file exists
    if not uploads_fs:exists(file.storage_path) then
        file_repo.update_status(file_id, "error")
        return nil, "File not found in storage"
    end

    -- Determine the content
    local content = nil
    local need_conversion = true

    -- Check if file is already text/markdown (no conversion needed)
    if file.mime_type == "text/markdown" or file.mime_type == "text/plain" then
        need_conversion = false

        -- Read file content directly
        content, err = uploads_fs:readfile(file.storage_path)
        if err then
            file_repo.update_status(file_id, "error")
            return nil, "Failed to read file: " .. err
        end
    else
        -- For other file types (PDF, DOCX, etc), convert to markdown first
        content, err = convert_to_markdown(file.storage_path, file.mime_type)
        if err then
            file_repo.update_status(file_id, "error")
            return nil, "Failed to convert file: " .. err
        end
    end

    -- Save full content to database
    local content_res, err = file_repo.save_content(file_id, content)
    if err then
        file_repo.update_status(file_id, "error")
        return nil, "Failed to save file content: " .. err
    end

    -- Configure chunking options
    local chunk_options = {
        min_chunk_size = 100,
        max_chunk_size = 1000,
        overlap_size = 50
    }

    -- Use markdown processor to chunk the content
    local chunks, hierarchy = markdown_processor.chunk_markdown(content, chunk_options)
    print(string.format("Created %d chunks from document", #chunks))

    -- Process sections from hierarchy
    local section_ids = {}
    local section_map = {}

    -- Add root section
    local root_section_id, _ = file_repo.add_section(file_id, "Document Root", 0, 0, nil)
    section_ids[0] = root_section_id
    section_map["Document Root"] = root_section_id

    -- Function to recursively process headings
    -- Function to recursively process headings
    local function process_headings(headings, parent_id, depth)
        for i, heading in ipairs(headings) do
            -- Ensure parent_id is a string, not a map or table
            local parent_id_str = parent_id
            if type(parent_id) == "table" then
                -- Extract the actual ID string if parent_id is a table
                if parent_id.section_id then
                    parent_id_str = parent_id.section_id
                else
                    print("Warning: parent_id is a table but doesn't have section_id field")
                    parent_id_str = nil
                end
            end

            local section_id, err = file_repo.add_section(
                file_id,
                heading.text,
                heading.level,
                i,
                parent_id_str  -- Now using the string representation
            )

            if err then
                print("Error adding section: " .. err)
            else
                section_ids[depth * 100 + i] = section_id
                section_map[heading.text] = section_id

                -- Process children recursively
                if heading.children and #heading.children > 0 then
                    process_headings(heading.children, section_id, depth + 1)
                end
            end
        end
    end

    -- Process all headings in the hierarchy
    process_headings(hierarchy, root_section_id, 1)

    -- Process chunks
    for i, chunk in ipairs(chunks) do
        -- Determine the section this chunk belongs to
        local section_id = root_section_id

        if chunk.metadata.current_section and chunk.metadata.current_section ~= "" then
            section_id = section_map[chunk.metadata.current_section] or root_section_id
        end

        -- Generate embedding for this chunk
        local embedding, err = generate_embedding(chunk.content)
        if err then
            print("Warning: Failed to generate embedding for chunk " .. i .. ": " .. err)
        end

        -- Add chunk to database
        local chunk_result, err = file_repo.add_chunk(
            file_id,
            section_id,
            chunk.content,
            "text",
            chunk.metadata.heading_path,
            embedding
        )

        if err then
            print("Error adding chunk: " .. err)
        end
    end

    -- Update file status to ready
    status, err = file_repo.update_status(file_id, "ready")
    if err then
        return nil, "Failed to update file status: " .. err
    end

    print(string.format("Successfully processed file %s", file_id))
    return {
        file_id = file_id,
        status = "ready",
        processed = true
    }
end

-- Process file in a separate process
function file_processor.start_processing(file_id)
    -- Start a process to handle the processing
    local child_pid = process.spawn(
        "app.files:file_processor.process", -- Process type - using the correct process ID from index
        "app:processes",                    -- Host to run on
        file_id                             -- File ID to process
    )

    if not child_pid then
        return nil, "Failed to start file processing process"
    end

    return {
        file_id = file_id,
        process_id = child_pid,
        started = true
    }
end

-- Run function for process
function file_processor.run(args)
    local file_id = args

    if not file_id or file_id == "" then
        return { error = "File ID is required" }
    end

    -- Process the file
    local result, err = file_processor.process(file_id)

    if err then
        file_repo.update_status(file_id, "error")
        return {
            error = err,
            file_id = file_id,
            success = false
        }
    end

    return {
        file_id = file_id,
        success = true
    }
end

return file_processor
