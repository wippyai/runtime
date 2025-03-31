local http = require("http")
local json = require("json")

local function handler()
    -- Get response object
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Import the models library
    local models_lib = require("models")

    -- Get all models from the models library
    local all_models = models_lib.get_all()

    -- Get models grouped by provider from the models library
    local grouped_providers = models_lib.get_by_provider()

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        count = #all_models,
        models = all_models,
        grouped = grouped_providers
    })
end

return {
    handler = handler
}
