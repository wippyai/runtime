local base64 = require("base64")

local function test_encode(): boolean
    local input: string = "hello world"
    local encoded: string = base64.encode(input)
    return encoded == "aGVsbG8gd29ybGQ="
end

local function test_decode(): boolean
    local encoded: string = "aGVsbG8gd29ybGQ="
    local decoded: string, err = base64.decode(encoded)
    return err == nil and decoded == "hello world"
end

local function test_roundtrip(): boolean
    local original: string = "test data 123!@#"
    local encoded: string = base64.encode(original)
    local decoded: string, err = base64.decode(encoded)
    return err == nil and decoded == original
end

local function main(): boolean
    return test_encode() and test_decode() and test_roundtrip()
end

return { main = main }
