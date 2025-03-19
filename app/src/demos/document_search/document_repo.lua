local sql = require("sql")
local llm = require("llm")

-- Database resource name
local DB_RESOURCE = "system:db"

local document_repo = {}

-- Get a database connection
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
    local response = llm.embed(text, {
        model = "text-embedding-3-small",
        dimensions = 512
    })

    if not response or response.error then
        return nil, "Failed to generate embedding: " .. (response and response.error_message or "Unknown error")
    end

    -- Format embedding as JSON array string for storage
    return "[" .. table.concat(response.result, ",") .. "]"
end

-- Add a new document
function document_repo.add(title, content)
    if not content or content == "" then
        return nil, "Content is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Start a transaction for atomicity
    local tx, err = db:begin()
    if err then
        db:release()
        return nil, "Failed to begin transaction: " .. err
    end

    -- Generate embedding for the document content
    local text_for_embedding = title .. "\n" .. content
    local embedding, err = generate_embedding(text_for_embedding)
    if err then
        tx:rollback()
        db:release()
        return nil, err
    end

    -- Insert into documents table with explicit timestamp cast to INTEGER
    local current_time = os.time()
    local result, err = tx:execute(
        "INSERT INTO documents(title, content, embedding, created_at) VALUES (?, ?, ?, CAST(? AS INTEGER))",
        { title or "", content, embedding, current_time }
    )
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to insert document: " .. err
    end

    local doc_id = result.last_insert_id

    -- Insert into full-text search table
    result, err = tx:execute(
        "INSERT INTO doc_content(doc_id, title, content) VALUES (?, ?, ?)",
        { doc_id, title or "", content }
    )
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to index document content: " .. err
    end

    -- Commit transaction
    local ok, err = tx:commit()
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to commit transaction: " .. err
    end

    db:release()
    return { id = doc_id }
end

-- Get a single document by ID
function document_repo.get(id)
    if not id or id <= 0 then
        return nil, "Invalid document ID"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT doc_id, title, content, created_at
        FROM documents
        WHERE doc_id = ?
    ]]

    local docs, err = db:query(query, { id })
    db:release()

    if err then
        return nil, "Failed to get document: " .. err
    end

    if #docs == 0 then
        return nil, "Document not found"
    end

    return docs[1]
end

-- List all documents
function document_repo.list(limit, offset)
    local db, err = get_db()
    if err then
        return nil, err
    end

    limit = limit or 100
    offset = offset or 0

    local query = [[
        SELECT doc_id, title, content, created_at
        FROM documents
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    ]]

    local docs, err = db:query(query, { limit, offset })
    db:release()

    if err then
        return nil, "Failed to list documents: " .. err
    end

    return docs
end

-- Search documents by text similarity (vector search)
function document_repo.search_by_similarity(query_text, limit)
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

    -- Perform vector similarity search
    local query = [[
        SELECT
            doc_id,
            title,
            content,
            created_at,
            distance  -- Similarity score
        FROM documents
        WHERE embedding MATCH ?
        AND k = ?    -- Return top k matches
        ORDER BY distance  -- Sort by similarity
    ]]

    local results, err = db:query(query, { embedding, limit })
    db:release()

    if err then
        return nil, "Search failed: " .. err
    end

    return results
end

-- BM25 text search with full FTS5 query syntax support
function document_repo.search_by_text(query_text, limit)
    if not query_text or query_text == "" then
        return nil, "Query text is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    limit = limit or 5

    -- Process the query text to support FTS5 advanced features
    local processed_query = process_fts5_query(query_text)

    -- Perform text search using FTS5 with BM25 ranking
    local query = [[
        SELECT
            c.doc_id,
            d.title,
            d.content,
            d.created_at,
            bm25(doc_content) AS relevance,
            highlight(doc_content, 0, '<mark>', '</mark>') AS content_highlight
        FROM doc_content c
        JOIN documents d ON c.doc_id = d.doc_id
        WHERE doc_content MATCH ']] .. processed_query .. [['
        ORDER BY relevance
        LIMIT ]] .. tostring(limit)

    local results, err = db:query(query)

    if err then
        db:release()
        return nil, "Text search failed: " .. err
    end

    db:release()
    return results
end

-- Process query text to enhance FTS5 query capabilities
-- This function handles:
-- 1. Proper escaping of quotes
-- 2. Automatic conversion of multi-word queries to AND queries if no operators present
-- 3. Preservation of FTS5 special syntax (phrases, operators, etc.)
function process_fts5_query(query_text)
    if not query_text or query_text == "" then
        return ""
    end

    -- First, escape any single quotes
    local escaped_query = query_text:gsub("'", "''")

    -- Check if the query already contains FTS5 operators
    local has_fts5_operators = escaped_query:match("[%+%-\":]") or
                               escaped_query:match("%sAND%s") or
                               escaped_query:match("%sOR%s") or
                               escaped_query:match("%sNOT%s") or
                               escaped_query:match("%sNEAR%s")

    -- If query has operators, keep as is (already using FTS5 syntax)
    if has_fts5_operators then
        return escaped_query
    end

    -- If no operators, split by whitespace and join with AND for better matching
    local words = {}
    for word in escaped_query:gmatch("%S+") do
        table.insert(words, word)
    end

    -- For single words, return as is
    if #words <= 1 then
        return escaped_query
    end

    -- For multiple words, join with AND for better matching
    return table.concat(words, " AND ")
end

-- Update hybrid search to use the same query processing
function document_repo.hybrid_search(query_text, limit)
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

    -- Process the query for FTS5
    local processed_query = process_fts5_query(query_text)

    -- Perform hybrid search with FTS5 query
    local query = [[
        WITH vector_matches AS (
            SELECT
                doc_id,
                distance
            FROM documents
            WHERE embedding MATCH ?
            AND k = 20
        ),
        text_matches AS (
            SELECT
                doc_id,
                bm25(doc_content) AS relevance
            FROM doc_content
            WHERE doc_content MATCH ']] .. processed_query .. [['
        )
        SELECT
            v.doc_id,
            d.title,
            d.content,
            d.created_at,
            v.distance AS vector_distance,
            t.relevance AS text_relevance,
            -- Combined ranking score (adjust weights as needed)
            (1 - (v.distance / 2)) * 0.6 + (1 / (t.relevance + 1)) * 0.4 AS score,
            highlight(doc_content, 0, '<mark>', '</mark>') AS content_highlight
        FROM vector_matches v
        JOIN text_matches t ON v.doc_id = t.doc_id
        JOIN documents d ON v.doc_id = d.doc_id
        ORDER BY score DESC
        LIMIT ]] .. tostring(limit)

    local results, err = db:query(query, { embedding })

    -- If there's an error with the hybrid search, fall back to vector search
    if err then
        db:release()
        -- Try vector search as fallback
        return document_repo.search_by_similarity(query_text, limit)
    end

    db:release()
    return results
end


return document_repo
