-- SPDX-License-Identifier: MPL-2.0

-- Workflow that tests crypto operations with side effects
-- Tests deterministic replay of random bytes, encryption, etc.
local crypto = require("crypto")

local function main(input: {message: string?})
	local results = {}

	-- Test random bytes generation (side effect)
	local bytes, err = crypto.random.bytes(16)
	if err then
		return nil, err
	end
	results.random_bytes_length = #bytes

	-- Test random string generation (side effect)
	local str, err = crypto.random.string(20)
	if err then
		return nil, err
	end
	results.random_string = str
	results.random_string_length = #str

	-- Test random UUID generation (side effect)
	local uuid, err = crypto.random.uuid()
	if err then
		return nil, err
	end
	results.random_uuid = uuid

	-- Test AES encryption (side effect due to nonce generation)
	local key = "0123456789abcdef" -- 16 bytes for AES-128
	local plaintext = input.message or "Hello, Temporal!"

	local ciphertext, err = crypto.encrypt.aes(plaintext, key)
	if err then
		return nil, err
	end
	results.ciphertext_length = #ciphertext

	-- Test AES decryption (deterministic)
	local decrypted, err = crypto.decrypt.aes(ciphertext, key)
	if err then
		return nil, err
	end
	results.decrypted = decrypted
	results.decrypt_matches = (decrypted == plaintext)

	-- Test ChaCha20 encryption (side effect due to nonce generation)
	local chacha_key = "01234567890123456789012345678901" -- 32 bytes
	local chacha_ciphertext, err = crypto.encrypt.chacha20(plaintext, chacha_key)
	if err then
		return nil, err
	end
	results.chacha_ciphertext_length = #chacha_ciphertext

	-- Test ChaCha20 decryption (deterministic)
	local chacha_decrypted, err = crypto.decrypt.chacha20(chacha_ciphertext, chacha_key)
	if err then
		return nil, err
	end
	results.chacha_decrypt_matches = (chacha_decrypted == plaintext)

	-- Test HMAC (deterministic - no side effect needed)
	local hmac_result, err = crypto.hmac.sha256("secret", plaintext)
	if err then
		return nil, err
	end
	results.hmac_sha256 = hmac_result

	-- Test PBKDF2 (deterministic - no side effect needed)
	local derived, err = crypto.pbkdf2("password", "salt", 1000, 32)
	if err then
		return nil, err
	end
	results.pbkdf2_length = #derived

	results.status = "completed"
	return results
end

return main
