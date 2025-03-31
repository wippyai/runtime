local http = require("http")
local json = require("json")
local document_repo = require("document_repo")

-- Add a new document with embedding
function add_document()
    local req = http.request()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    -- Parse request body
    local body = req:body()
    if not body or body == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({ error = "Empty request body" })
        return
    end

    local data, err = json.decode(body)
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({ error = "Invalid JSON: " .. err })
        return
    end

    if not data.content then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({ error = "Content is required" })
        return
    end

    local result, err = document_repo.add(data.title, data.content)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({ error = err })
        return
    end

    res:set_status(http.STATUS.CREATED)
    res:write_json({ id = result.id, success = true })
end

return {
    add_document = add_document
}
