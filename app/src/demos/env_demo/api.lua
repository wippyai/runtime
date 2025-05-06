local envstorage = require("envstorage")

-- Main handler function
local function handler()
    local actual_api_key = nil
    local actual_api_url = nil

    actual_api_key = envstorage.get('openai_api_key')
    actual_api_url = envstorage.get('openai_api_url')

    print("actual_api_key:", actual_api_key)
    print("actual_api_url:", actual_api_url)
end

return {
    handler = handler
}
