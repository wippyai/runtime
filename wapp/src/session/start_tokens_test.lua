local test = require("test")
local start_tokens = require("start_tokens")
local time = require("time")
local crypto = require("crypto")
local base64 = require("base64")
local json = require("json")

-- Define test suite
local tests = test.new("Start Tokens Tests")

-- Test valid token creation and unpacking
tests:case("Create and unpack valid token", function()
    -- Create a token with valid params
    local params = {
        agent = "test_agent",
        model = "claude-3-7-sonnet",
        kind = "test_session"
    }

    local token, err = start_tokens.pack(params)
    test.is_nil(err, "No error should be returned when creating valid token")
    test.is_string(token, "Token should be a string")

    -- Unpack the token
    local result, err = start_tokens.unpack(token)
    test.is_nil(err, "No error should be returned when unpacking valid token")
    test.is_table(result, "Result should be a table")

    -- Verify contents match
    test.equal(result.agent, params.agent, "Agent should match")
    test.equal(result.model, params.model, "Model should match")
    test.equal(result.kind, params.kind, "Kind should match")
    test.is_number(result.issued_at, "issued_at should be a timestamp")
end)

-- Test minimal params (omit kind)
tests:case("Create token with minimal params", function()
    local params = {
        agent = "minimal_agent",
        model = "gpt-4o"
    }

    local token, err = start_tokens.pack(params)
    test.is_nil(err, "No error should be returned with minimal params")

    local result, err = start_tokens.unpack(token)
    test.is_nil(err, "No error should be returned when unpacking")
    test.equal(result.agent, params.agent, "Agent should match")
    test.equal(result.model, params.model, "Model should match")
    test.equal(result.kind, "", "Kind should default to empty string")
end)

-- Test error handling: missing required params
tests:case("Error on missing required params", function()
    -- Missing agent
    local token1, err1 = start_tokens.pack({model = "test-model"})
    test.is_nil(token1, "Token should be nil with missing agent")
    test.is_string(err1, "Error should be returned for missing agent")
    test.match(err1, "Agent name is required", "Error should mention missing agent")

    -- Missing model
    local token2, err2 = start_tokens.pack({agent = "test-agent"})
    test.is_nil(token2, "Token should be nil with missing model")
    test.is_string(err2, "Error should be returned for missing model")
    test.match(err2, "Model name is required", "Error should mention missing model")

    -- Invalid params type
    local token3, err3 = start_tokens.pack("not a table")
    test.is_nil(token3, "Token should be nil with non-table params")
    test.is_string(err3, "Error should be returned for non-table params")
    test.match(err3, "Parameters must be provided as a table", "Error should mention table requirement")
end)

-- Test error handling: invalid token formats
tests:case("Error on invalid token format", function()
    -- Nil token
    local result1, err1 = start_tokens.unpack(nil)
    test.is_nil(result1, "Result should be nil with nil token")
    test.is_string(err1, "Error should be returned for nil token")
    test.match(err1, "No token provided", "Error should mention missing token")

    -- Non-base64 token
    local result2, err2 = start_tokens.unpack("not a valid token")
    test.is_nil(result2, "Result should be nil with invalid base64")
    test.is_string(err2, "Error should be returned for invalid base64")
    test.match(err2, "Invalid token format", "Error should mention format issue")

    -- Valid base64 but not our token
    local result3, err3 = start_tokens.unpack(base64.encode("just some random data"))
    test.is_nil(result3, "Result should be nil with invalid encrypted data")
    test.is_string(err3, "Error should be returned for invalid encrypted data")
end)

-- Test token expiration
tests:case("Detect expired tokens", function()
    -- Create a token with a manually constructed payload
    local payload = {
        agent = "expired_agent",
        model = "expired_model",
        kind = "expired_session",
        issued_at = os.time() - 90000  -- Set issue time to 25 hours ago (86400 + 3600)
    }

    -- Encode, encrypt and base64 manually to bypass the normal timestamp setting
    local json_data = json.encode(payload)
    local encrypted = crypto.encrypt.aes(json_data, "Antares_Chat_StartToken_Secret_Key")
    local token = base64.encode(encrypted)

    -- Try to unpack the expired token
    local result, err = start_tokens.unpack(token)
    test.is_nil(result, "Result should be nil for expired token")
    test.is_string(err, "Error should be returned for expired token")
    test.match(err, "Token expired", "Error should mention expiration")
end)

-- Execute all tests
function run_tests()
    return tests:run()
end

return {
    run_tests = run_tests
}