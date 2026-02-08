local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:crypto_workflow",
		"app.test.temporal:test_worker",
		{ message = "Temporal crypto" }
	)

	assert.is_nil(err, "crypto workflow exec should not error")
	assert.is_table(result, "result table")
	assert.eq(result.status, "completed", "status completed")
	assert.eq(result.random_bytes_length, 16, "random bytes length")
	assert.eq(result.random_string_length, 20, "random string length")
	assert.ok(result.decrypt_matches, "aes decrypt matches")
	assert.ok(result.chacha_decrypt_matches, "chacha decrypt matches")
	assert.is_string(result.hmac_sha256, "hmac string")
	assert.eq(result.pbkdf2_length, 32, "pbkdf2 length")

	return true
end

return { main = main }
