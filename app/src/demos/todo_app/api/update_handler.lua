local http = require("http")
local json = require("json")
local todo_repo = require("todo_repo")

-- Update a todo
function update_todo()
    local req = http.request()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    local id = tonumber(req:query("id"))
    if not id then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "Missing or invalid todo ID"})
        return
    end

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

    local result, err = todo_repo.update(id, data.title, data.note)
    if err then
        if err == "Todo not found" then
            res:set_status(http.STATUS.NOT_FOUND)
        else
            res:set_status(http.STATUS.INTERNAL_ERROR)
        end
        res:write_json({error = err})
        return
    end

    res:write_json({id = result.id, success = true})
end

return {
    update_todo = update_todo
}