local crypto = require("crypto")
local base64 = require("base64")

-- Hardcoded encryption key - in production this should be securely managed
local SESSION_KEY = "Antares_Chat_Session_Encryption_"

-- Encrypt session PID for HTTP transport
local function encrypt_session_pid(pid)
    if not pid then return nil, "No PID provided" end

    -- Encrypt the PID using AES-GCM from the crypto module
    local encrypted, err = crypto.encrypt.aes(pid, SESSION_KEY)
    if err then
        return nil, "Encryption error: " .. err
    end

    -- Base64 encode the encrypted data for HTTP transport
    return base64.encode(encrypted)
end

-- Decrypt and validate session token from HTTP request
local function decrypt_session_pid(token)
    if not token then return nil, "No token provided" end

    -- Decode base64 first
    local encrypted_data = base64.decode(token)
    if not encrypted_data then
        return nil, "Invalid token format"
    end

    -- Decrypt the data
    local pid, err = crypto.decrypt.aes(encrypted_data, SESSION_KEY)
    if err then
        return nil, "Invalid session token: " .. err
    end

    -- Validate the PID format
    if not pid:match("^{Antares@.+:.+|.+}$") then
        return nil, "Malformed session identifier"
    end

    return pid
end

return {
    encrypt_session_pid = encrypt_session_pid,
    decrypt_session_pid = decrypt_session_pid
}