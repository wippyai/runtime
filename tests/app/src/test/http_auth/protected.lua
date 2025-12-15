local function handler()
    local http = require("http")
    local security = require("security")

    local res = http.response()

    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({ error = "not authenticated" })
        return
    end

    local meta = actor:meta()
    res:set_status(http.STATUS.OK)
    res:write_json({
        message = "access granted",
        actor_id = actor:id(),
        actor_type = meta and meta.type or nil
    })
end

return { handler = handler }
