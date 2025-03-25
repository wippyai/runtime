local sql = require("sql")
local json = require("json")
local llm = require("llm")

-- Constants
local DB_RESOURCE = "app:db"
local EMBEDDING_MODEL = "text-embedding-3-small"
local EMBEDDING_DIMENSIONS = 512

local chunk_query = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Generate embedding for query text
local function generate_embedding(text)
    -- Use embedding model to generate vector
    local response = llm.embed(text, {
        model = EMBEDDING_MODEL,
        dimensions = EMBEDDING_DIMENSIONS
    })

    if not response or response.error then
        return nil, "Failed to generate embedding: " .. (response and response.error_message or "Unknown error")
    end

    -- Format embedding as JSON array string for vector search
    return "[" .. table.concat(response.result, ",") .. "]"
end

-- Query chunks by semantic similarity
function chunk_query.query(file_id, query_string, limit)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not query_string or query_string == "" then
        return nil, "Query string is required"
    end

    limit = limit or 5

    -- Generate embedding for the query
    local embedding, err = generate_embedding(query_string)
    if err then
        return nil, "Embedding generation failed: " .. err
    end

    local db, err = get_db()
    if err then
        return nil, err
    end
print(file_id)
    local query = [[
        SELECT
            file_id,
            chunk_id,
            content,
            path,
            distance
        FROM file_chunks
        WHERE embedding MATCH ?
        AND k = ?

        ORDER BY distance
    ]]

    local chunks, err = db:query(query, { embedding, sql.as.int(limit) })
    db:release()

    if err then
        return nil, "Semantic search failed: " .. err
    end

    -- Parse JSON paths for better usability
    for i, chunk in ipairs(chunks) do
        local success, path_obj = pcall(json.decode, chunk.path)
        if success then
            chunk.path_object = path_obj
        end
    end

    return chunks
end

return chunk_query
