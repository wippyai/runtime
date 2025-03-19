local crypto = require("crypto")
local base64 = require("base64")
local json = require("json")

-- Hardcoded encryption key - in production this should be securely managed
local START_TOKEN_KEY = "Chat_StartToken_Secret_Key_____1"

-- Pack session parameters table into an encrypted start token
local function pack_start_token(params)
    if type(params) ~= "table" then
        return nil, "Parameters must be provided as a table"
    end

    if not params.agent then return nil, "Agent name is required" end
    if not params.model then return nil, "Model name is required" end

    -- Create a payload object (clone to avoid modifying the input)
    local payload = {
        agent = params.agent,
        model = params.model,
        kind = params.kind or "",
        issued_at = os.time()
    }

    -- Serialize to JSON
    local json_data, err = json.encode(payload)
    if err then
        return nil, "Failed to encode payload: " .. err
    end

    -- Encrypt the payload using AES-GCM from the crypto module
    local encrypted, err = crypto.encrypt.aes(json_data, START_TOKEN_KEY)
    if err then
        return nil, "Encryption error: " .. err
    end

    -- Base64 encode the encrypted data for HTTP transport
    return base64.encode(encrypted)
end

-- Unpack a start token into the original session parameters table
local function unpack_start_token(token)
    if not token then return nil, "No token provided" end

    -- Decode base64 first
    local encrypted_data = base64.decode(token)
    if not encrypted_data then
        return nil, "Invalid token format"
    end

    -- Decrypt the data
    local json_data, err = crypto.decrypt.aes(encrypted_data, START_TOKEN_KEY)
    if err then
        return nil, "Invalid start token: " .. err
    end

    -- Parse JSON
    local payload, err = json.decode(json_data)
    if err then
        return nil, "Malformed token payload: " .. err
    end

    -- Validate token isn't too old (optional, 24 hour expiry)
    local current_time = os.time()
    local issued_at = payload.issued_at or 0
    local token_age = current_time - issued_at

    if token_age > 86400 then -- 24 hours in seconds
        return nil, "Token expired"
    end

    -- Return the parameters as a table
    return {
        agent = payload.agent,
        model = payload.model,
        kind = payload.kind,
        issued_at = payload.issued_at
    }
end

return {
    pack = pack_start_token,
    unpack = unpack_start_token
}
