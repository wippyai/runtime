local http = require("http")
local json = require("json")
local models_lib = require("models")

-- Helper function to split string
function string:split(sep)
    local fields = {}
    local pattern = string.format("([^%s]+)", sep)
    self:gsub(pattern, function(c) fields[#fields + 1] = c end)
    return fields
end

local function handler()
    -- Get response object
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Get query parameter with model names
    local models_param = req:query("models")
    if not models_param or models_param == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:set_content_type(http.CONTENT.JSON)
        res:write_json({
            success = false,
            error = "Missing 'models' query parameter. Expected comma-separated list of model names."
        })
        return
    end

    -- Split comma-separated list of model names
    local model_names = models_param:split(",")

    -- Get all models from the models library
    local all_models = models_lib.get_all()

    -- Create a lookup map of model name to model object
    local model_map = {}
    for _, model in ipairs(all_models) do
        model_map[model.name] = model
    end

    -- Map requested names to full model information
    local result = {}
    for _, name in ipairs(model_names) do
        if model_map[name] then
            result[name] = {
                name = model_map[name].name,
                title = model_map[name].title or model_map[name].name,
                provider = model_map[name].provider
            }
        else
            result[name] = nil
        end
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        models = result
    })
end

return {
    handler = handler
}
