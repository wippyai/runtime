local http = require("http")
local todo_repo = require("todo_repo")

-- List all todos
function list_todos()
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    local todos, err = todo_repo.list()
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({error = err})
        return
    end

    res:write_json({todos = todos})
end

return {
    list_todos = list_todos
}