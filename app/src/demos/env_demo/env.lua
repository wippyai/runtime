local env = require("env")

-- Main handler function
local function handler()
    local actual_api_key = nil
    local actual_api_url = nil
    local actual_document_loaders_host = nil

    actual_api_key = env.get('openai_api_key')
    actual_api_url = env.get('openai_api_url')
    actual_document_loaders_host = env.get('document_loaders_host')

    print("actual_api_key:", actual_api_key)
    print("actual_api_url:", actual_api_url)
    print("actual_document_loaders_host:", actual_document_loaders_host)
end

return {
    handler = handler
}