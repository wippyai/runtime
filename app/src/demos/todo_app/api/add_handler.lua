local http = require("http")
local json = require("json")
local todo_repo = require("todo_repo")

-- Create a new todo
function add_todo()
    local req = http.request()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    -- Parse request body
    local body = req:body()
    if not body or body == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "Empty request body"})
        return
    end

    local data, err = json.decode(body)
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "Invalid JSON: " .. err})
        return
    end

    if not data.title then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "Title is required"})
        return
    end

    local result, err = todo_repo.add(data.title, data.note)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({error = err})
        return
    end

    res:set_status(http.STATUS.CREATED)
    res:write_json({id = result.id, success = true})
end

return {
    add_todo = add_todo
}