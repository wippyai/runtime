local assert = require("assert2")
local http = require("http_client")

local function main()
	local res, err = http.post("http://localhost:8085/wasm/greet", {
		body = "Wippy",
		headers = {
			["Content-Type"] = "text/plain"
		}
	})
	assert.is_nil(err, "wasi-http endpoint call should not error")
	assert.not_nil(res, "wasi-http endpoint response should exist")
	assert.eq(res.status_code, 200, "wasi-http endpoint status should be 200")
	assert.eq(res.body, "Hello, Wippy!", "wasi-http endpoint body should map wasm result")

	return true
end

return { main = main }