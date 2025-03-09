local http = require("http")
local todo_repo = require("todo_repo")

-- Get a single todo
function get_todo()
    local req = http.request()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    local id = tonumber(req:query("id"))
    if not id then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({error = "Missing or invalid todo ID"})
        return
    end

    local todo, err = todo_repo.get(id)
    if err then
        if err == "Todo not found" then
            res:set_status(http.STATUS.NOT_FOUND)
        else
            res:set_status(http.STATUS.INTERNAL_ERROR)
        end
        res:write_json({error = err})
        return
    end

    res:write_json({todo = todo})
end

return {
    get_todo = get_todo
}