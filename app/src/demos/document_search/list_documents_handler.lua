local http = require("http")
local document_repo = require("document_repo")

-- List all documents
function list_documents()
    local req = http.request()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    local limit = tonumber(req:query("limit")) or 100
    local offset = tonumber(req:query("offset")) or 0

    local documents, err = document_repo.list(limit, offset)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({error = err})
        return
    end

    res:write_json({documents = documents})
end

return {
    list_documents = list_documents
}