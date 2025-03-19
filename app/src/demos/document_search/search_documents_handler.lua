local http = require("http")
local document_repo = require("document_repo")

-- Search documents by text similarity
function search_documents()
    local req = http.request()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    local query_text = req:query("q")
    if not query_text or query_text == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "Query text is required"})
        return
    end

    local limit = tonumber(req:query("limit")) or 5

    -- Determine search type: vector, bm25, or hybrid
    local search_type = req:query("type") or "hybrid"

    local results, err

    if search_type == "hybrid" then
        results, err = document_repo.hybrid_search(query_text, limit)
    elseif search_type == "bm25" then
        results, err = document_repo.search_by_text(query_text, limit)
    else
        -- Default to vector search
        results, err = document_repo.search_by_similarity(query_text, limit)
    end

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({error = err})
        return
    end

    res:write_json({results = results})
end

return {
    search_documents = search_documents
}