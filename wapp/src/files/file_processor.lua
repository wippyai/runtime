local fs = require("fs")
local llm = require("llm")
local json = require("json")
local time = require("time")
local http_client = require("http_client")
local chunk_embed_processor = require("chunk_embed_processor")
local file_repo = require("file_repo")

local file_processor = {}

-- Maximum tokens for embedding model
local MAX_EMBEDDING_TOKENS = 8000 -- Set a safe limit below the 8192 maximum

-- Process embeddings (updated to use chunking process)
function file_processor.process_embeddings(file_id, content)
    print("Starting embedding process for file " .. file_id)



    -- We can run this in blocking mode since we're already in a separate process
    local result, err = chunk_embed_processor.process(file_id)

    if err then
        print("Error during chunking and embedding: " .. err)
        file_repo.update_status(file_id, "error")
        return false, err
    end

    print(string.format("File %s successfully chunked and embedded: %d chunks created, %d embedded, %d saved",
        file_id, result.chunks_created, result.chunks_embedded, result.chunks_saved))

    -- The chunk_embed_processor already updates status to "ready"
    return true
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

    -- Process embeddings in a simplified way for testing
    file_processor.process_embeddings(file_id, content)

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
