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

    -- test read/write memory variables
    local memory_test_env = nil
    actual_api_key = env.get('memory_test_env')
    print("memory_test_env:", memory_test_env)

    -- test set memory variable
    env.set('memory_test_env', 'actual_api_key')
    memory_test_env = env.get('memory_test_env')
    print("updated memory_test_env:", memory_test_env)  

    -- test read/write memory readonly variable
    local memory_test_env_readonly = nil
    memory_test_env_readonly = env.get('memory_test_env_readonly')
    print("memory_test_env_readonly:", memory_test_env_readonly)

    -- Try to set readonly variable (this should fail)
    local success, err = env.set('memory_test_env_readonly', 'new_value')
    if not success then
        print("Failed to set readonly variable:", err)
    end

    -- file variables
    -- test read/write file variables
    local file_test_env = nil
    file_test_env = env.get('file_test_env')
    print("file_test_env:", file_test_env)

    -- test set file variable
    env.set('file_test_env', 'file_value')
    file_test_env = env.get('file_test_env')
    print("updated file_test_env:", file_test_env)

    -- test read/write file readonly variable
    local file_test_env_readonly = nil
    file_test_env_readonly = env.get('file_test_env_readonly')
    print("file_test_env_readonly:", file_test_env_readonly)

    -- Try to set readonly file variable (this should fail)
    local success, err = env.set('file_test_env_readonly', 'new_file_value')
    if not success then
        print("Failed to set readonly file variable:", err)
    end

    print("ok")
end

return {
    handler = handler
}