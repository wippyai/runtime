local llm = require("llm")
local json = require("json")
local sql = require("sql")
local prompt = require("prompt")

local file_repo = require("file_repo")

-- Database resource name
local DB_RESOURCE = "app:db"

-- Helper function to get database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Generate embedding for text
local function generate_embedding(text)
    -- Use text-embedding-3-small model for embeddings
    local response = llm.embed({
        model = "text-embedding-3-small",
        input = text,
        dimensions = 512
    })

    if not response or response.error then
        return nil, "Failed to generate embedding: " .. (response and response.error_message or "Unknown error")
    end

    -- Return embedding array
    return "[" .. table.concat(response.result, ",") .. "]"
end

-- Search for relevant chunks in a file
local function search_chunks(file_id, query_text, limit)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not query_text or query_text == "" then
        return nil, "Query text is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    limit = limit or 5

    -- Generate embedding for the query text
    local embedding, err = generate_embedding(query_text)
    if err then
        db:release()
        return nil, err
    end

    -- Format embedding for SQLite query
    local embedding_json = "[" .. table.concat(embedding, ",") .. "]"

    -- Perform vector similarity search against chunks
    local query = [[
        SELECT
            chunk_id,
            file_id,
            section_id,
            content,
            type,
            distance  -- Similarity score
        FROM file_chunks
        WHERE file_id = ?
        AND embedding MATCH ?
        AND k = ?    -- Return top k matches
        ORDER BY distance  -- Sort by similarity
    ]]

    local results, err = db:query(query, { file_id, embedding_json, limit })
    db:release()

    if err then
        return nil, "Search failed: " .. err
    end

    return results
end

-- Query an LLM with retrieved context
local function query_llm(query_text, chunks, file_info)
    -- Create a prompt builder
    local builder = prompt.new()

    -- Prepare context with citation information
    local context = ""
    local references = {}

    for i, chunk in ipairs(chunks) do
        -- Use chunk_id as reference
        local ref_id = "[" .. i .. "]"

        -- Add to context with reference ID
        context = context .. "--- Passage " .. ref_id .. " ---\n" .. chunk.content .. "\n\n"

        -- Save reference information
        table.insert(references, {
            id = ref_id,
            source = "Document Chunk " .. i
        })
    end

    -- Format references section
    local references_text = "References:\n"
    for _, ref in ipairs(references) do
        references_text = references_text .. ref.id .. ": " .. ref.source .. "\n"
    end

    -- Add system instructions
    builder:add_system([[You are a helpful AI assistant analyzing a document.
Use the provided context to answer the user's question about the document.
If the question cannot be answered based on the provided context, say so clearly.
Your answers should be factual and based only on the provided context.

IMPORTANT: You must cite your sources by using the reference numbers [1], [2], etc. from the passages.
Include at least one citation for each claim you make in your answer.
You can cite multiple references if a claim comes from multiple passages.

Document Information:
Title: ]] .. file_info.filename .. [[

Context from the document:
]] .. context .. [[

]] .. references_text)

    -- Add user query
    builder:add_user(query_text)

    -- Generate answer using the prompt builder
    local response = llm.generate(builder, {
        model = "o3-mini",
        options = {
            temperature = 0.2,
            max_tokens = 1000
        }
    })

    if not response or response.error then
        return nil, "Failed to generate answer: " .. (response and response.error_message or "Unknown error")
    end

    return response.result
end

-- Main RAG function
local function run(args)
    -- Set default options if not provided
    args = args or {}
    -- Validate required arguments
    if not args then
        return { success = false, error = "Arguments required" }
    end

    local file_id = args.file_id
    local query = args.query
    local user_id = args.user_id

    if not file_id or file_id == "" then
        return { success = false, error = "File ID is required" }
    end

    if not query or query == "" then
        return { success = false, error = "Query is required" }
    end

    if not user_id or user_id == "" then
        return { success = false, error = "User ID is required" }
    end

    -- Get file information first to check ownership
    local file, err = file_repo.get(file_id)
    if err then
        return {
            success = false,
            error = "File not found",
            details = err
        }
    end

    -- Check if the file belongs to the user
    if file.user_id ~= user_id then
        return {
            success = false,
            error = "Access denied",
            details = "You do not have permission to access this file"
        }
    end

    -- Search for relevant chunks
    local chunks, err = search_chunks(file_id, query, 5)
    if err then
        return {
            success = false,
            error = "Search failed",
            details = err
        }
    end

    -- If no chunks found
    if #chunks == 0 then
        return {
            success = true,
            answer = "No relevant content found in the document to answer this question.",
            chunks = {},
            fact_id = nil
        }
    end

    -- Query LLM with retrieved context and chunks (for citation)
    local answer, err = query_llm(query, chunks, file)
    if err then
        return {
            success = false,
            error = "Answer generation failed",
            details = err
        }
    end

    -- Save fact to database
    local fact, err = file_repo.add_fact(file_id, query, answer)
    if err then
        -- Continue even if saving fact fails
        print("Warning: Failed to save fact: " .. err)
    end

    -- Return successful result
    return {
        success = true,
        answer = answer,
        chunks = chunks,
        fact_id = fact and fact.fact_id or nil
    }
end

return {
    run = run
}
