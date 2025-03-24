local fs = require("fs")
local llm = require("llm")
local json = require("json")
local time = require("time")

local file_repo = require("file_repo")

local chunk_embed_processor = {}

-- Constants for chunking and embedding
local MAX_CHUNKS_PER_BATCH = 20
local MAX_TOKENS_PER_REQUEST = 7000
local MIN_CHUNK_SIZE = 100
local MAX_CHUNK_SIZE = 500
local OVERLAP_SIZE = 50
local EMBEDDING_MODEL = "text-embedding-3-small"
local EMBEDDING_DIMENSIONS = 512

-- Remove base64 images from markdown content
local function remove_base64_images(content)
    -- This pattern matches Markdown image syntax with base64 data
    local pattern = "!%[.-%]%(data:image/[^;]-;base64,[^%)]+%)"

    -- Replace the pattern with a placeholder
    return string.gsub(content, pattern, "[IMAGE REMOVED]")
end

-- More conservative token estimation (1 token = ~3 characters)
local function estimate_tokens(text)
    return math.ceil(#text / 3)
end

-- Simple text chunking function that respects markdown structure
local function chunk_markdown(content)
    if not content or #content == 0 then
        return {}
    end

    -- First, remove base64 images to reduce token count
    content = remove_base64_images(content)

    local chunks = {}
    local lines = {}

    -- Split content into lines
    for line in string.gmatch(content, "([^\r\n]+)\n?") do
        table.insert(lines, line)
    end

    local current_chunk = {}
    local current_size = 0
    local heading_path = {}
    local current_section = "Document Root"
    local i = 1

    while i <= #lines do
        local line = lines[i]
        local line_length = #line

        -- Check if line starts a header (# or ## or ###)
        local heading_level = 0
        for j = 1, 6 do
            if string.match(line, "^%s*" .. string.rep("#", j) .. "%s+") then
                heading_level = j
                break
            end
        end

        -- If this is a heading, update our heading path
        if heading_level > 0 then
            local heading_text = string.match(line, "^#+%s+(.+)$") or ""

            -- Update heading path based on level
            while #heading_path >= heading_level do
                table.remove(heading_path)
            end
            table.insert(heading_path, heading_text)
            current_section = heading_text
        end

        -- If we're at a header and already have content, or the chunk would get too big, start a new chunk
        if (heading_level > 0 and current_size > MIN_CHUNK_SIZE) or (current_size + line_length > MAX_CHUNK_SIZE) then
            -- Only save chunk if it has content
            if current_size > 0 then
                local chunk_text = table.concat(current_chunk, "\n")

                -- Create path info
                local path_info = {
                    section_path = table.concat(heading_path, " > "),
                    title = current_section,
                    section_index = #chunks + 1
                }

                table.insert(chunks, {
                    content = chunk_text,
                    path = json.encode(path_info)
                })

                -- If we're at a header, start fresh
                if heading_level > 0 then
                    current_chunk = {line}
                    current_size = line_length
                else
                    -- For normal text, add some overlap from the previous chunk
                    local overlap_start = math.max(1, #current_chunk - math.ceil(OVERLAP_SIZE / 30))
                    current_chunk = {}

                    -- Add overlapping lines
                    for j = overlap_start, #current_chunk do
                        table.insert(current_chunk, current_chunk[j])
                    end

                    -- Add the current line
                    table.insert(current_chunk, line)
                    current_size = line_length
                end
            else
                -- If current chunk is empty, just add the line
                table.insert(current_chunk, line)
                current_size = line_length
            end
        else
            -- Add line to current chunk
            table.insert(current_chunk, line)
            current_size = current_size + line_length
        end

        i = i + 1
    end

    -- Add the last chunk if it has content
    if #current_chunk > 0 then
        local chunk_text = table.concat(current_chunk, "\n")

        -- Create path info for the last chunk
        local path_info = {
            section_path = table.concat(heading_path, " > "),
            title = current_section,
            section_index = #chunks + 1
        }

        table.insert(chunks, {
            content = chunk_text,
            path = json.encode(path_info)
        })
    end

    return chunks
end

-- Generate embeddings in batches
local function batch_embed_chunks(chunks)
    if not chunks or #chunks == 0 then
        return {}, "No chunks to embed"
    end

    local batches = {}
    local current_batch = {}
    local current_token_count = 0
    local current_chunk_count = 0

    -- Create batches respecting limits
    for _, chunk in ipairs(chunks) do
        local chunk_tokens = estimate_tokens(chunk.content)

        -- Skip chunks that exceed the max token limit (unlikely but a safeguard)
        if chunk_tokens > MAX_TOKENS_PER_REQUEST then
            print("Warning: Chunk exceeds maximum token limit, skipping")
            goto continue
        end

        -- Check if adding this chunk would exceed batch limits
        if current_chunk_count >= MAX_CHUNKS_PER_BATCH or
           current_token_count + chunk_tokens > MAX_TOKENS_PER_REQUEST then
            -- Save current batch and start a new one
            table.insert(batches, current_batch)
            current_batch = {}
            current_token_count = 0
            current_chunk_count = 0
        end

        -- Add chunk to current batch
        table.insert(current_batch, {
            content = chunk.content,
            path = chunk.path
        })
        current_token_count = current_token_count + chunk_tokens
        current_chunk_count = current_chunk_count + 1

        ::continue::
    end

    -- Add the last batch if not empty
    if #current_batch > 0 then
        table.insert(batches, current_batch)
    end

    -- Now process each batch with the LLM API
    local results = {}

    for batch_index, batch in ipairs(batches) do
        local batch_texts = {}

        -- Extract just the text content for embedding
        for _, chunk in ipairs(batch) do
            table.insert(batch_texts, chunk.content)
        end

        print(string.format("Processing batch %d of %d (%d chunks)",
                           batch_index, #batches, #batch))

        -- Call the embedding API
        local response = llm.embed(batch_texts, {
            model = EMBEDDING_MODEL,
            dimensions = EMBEDDING_DIMENSIONS
        })

        if not response or response.error then
            print(string.format("Error in batch %d: %s",
                  batch_index, response and response.error_message or "Unknown error"))
            goto continue
        end

        -- Add embeddings to results
        for i, embedding in ipairs(response.result) do
            results[#results + 1] = {
                text = batch[i].content,
                embedding = embedding,
                path = batch[i].path
            }
        end

        -- Add a small delay between batches to avoid rate limiting
        if batch_index < #batches then
            time.sleep(0.5)
        end

        ::continue::
    end

    return results
end

-- Process file content into chunks with embeddings
function chunk_embed_processor.process(file_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    -- Get file info
    local file, err = file_repo.get(file_id)
    if err then
        return nil, "Failed to get file: " .. err
    end

    -- Check if file status is appropriate for processing
    if file.status ~= "ready" and file.status ~= "processing" then
        return nil, "File is not ready for chunking and embedding"
    end

    -- Update file status to indicate chunking is in progress
    local status_res, err = file_repo.update_status(file_id, "chunking")
    if err then
        return nil, "Failed to update file status: " .. err
    end

    -- Get file content
    local content, err = file_repo.get_content(file_id)
    if err then
        file_repo.update_status(file_id, "error")
        return nil, "Failed to get file content: " .. err
    end

    -- Chunk the content
    print(string.format("Chunking content for file %s", file_id))
    local chunks = chunk_markdown(content)
    print(string.format("Created %d chunks", #chunks))

    if #chunks == 0 then
        file_repo.update_status(file_id, "error")
        return nil, "Failed to create chunks from content"
    end

    -- Generate embeddings in batches
    print(string.format("Generating embeddings for %d chunks", #chunks))
    local chunk_embeddings = batch_embed_chunks(chunks)
    print(string.format("Generated embeddings for %d chunks", #chunk_embeddings))

    if #chunk_embeddings == 0 then
        file_repo.update_status(file_id, "error")
        return nil, "Failed to generate embeddings"
    end

    -- Save chunks with embeddings to database
    print("Saving chunks to database")
    local success_count = 0

    for i, chunk_data in ipairs(chunk_embeddings) do
        -- Format embedding as JSON string for storage
        local embedding_json = "[" .. table.concat(chunk_data.embedding, ",") .. "]"

        -- Add chunk to database
        local chunk_res, err = file_repo.add_chunk(
            file_id,
            nil, -- No section_id
            chunk_data.text,
            "markdown",
            chunk_data.path,
            embedding_json
        )

        if chunk_res then
            success_count = success_count + 1
        else
            print(string.format("Error adding chunk %d: %s", i, err or "Unknown error"))
        end
    end

    print(string.format("Successfully stored %d/%d chunks", success_count, #chunk_embeddings))

    -- Update file status to ready
    file_repo.update_status(file_id, "ready")

    return {
        file_id = file_id,
        success = true,
        chunks_created = #chunks,
        chunks_embedded = #chunk_embeddings,
        chunks_saved = success_count
    }
end

-- Run function for the process
function chunk_embed_processor.run(args)
    local file_id = args

    if not file_id or file_id == "" then
        return { error = "File ID is required" }
    end

    print(string.format("Starting chunking and embedding process for file %s", file_id))

    -- Process the file
    local result, err = chunk_embed_processor.process(file_id)

    if err then
        print(string.format("Error processing file %s: %s", file_id, err))
        file_repo.update_status(file_id, "error")
        return {
            error = err,
            file_id = file_id,
            success = false
        }
    end

    print(string.format("Successfully processed file %s", file_id))
    return {
        file_id = file_id,
        success = true,
        chunks_created = result.chunks_created,
        chunks_embedded = result.chunks_embedded,
        chunks_saved = result.chunks_saved
    }
end

return chunk_embed_processor