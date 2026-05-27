-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	local login_res, err = http.post("http://localhost:8085/auth/login", {
		body = json.encode({ username = "queryuser" }),
		headers = { ["Content-Type"] = "application/json" }
	})
	if err then
		return false, "login request error: " .. tostring(err)
	end

	if login_res.status_code ~= 200 then
		return false, "login failed with status " .. login_res.status_code .. ": " .. (login_res.body or "")
	end

	assert.not_nil(login_res.body, "login response body present")
	local login_body = json.decode(tostring(login_res.body))
	assert.not_nil(login_body, "login response should be valid JSON")
	assert.not_nil(login_body.token, "login response should contain token")

	local token = login_body.token

	local url = "http://localhost:8085/auth/protected?x-auth-token=" .. token
	local protected_res, err = http.get(url)
	if err then
		return false, "protected request error: " .. tostring(err)
	end

	assert.eq(protected_res.status_code, 200, "should return 200 OK with query token")
	assert.not_nil(protected_res.body, "protected response body present")

	local protected_body = json.decode(tostring(protected_res.body))
	assert.not_nil(protected_body, "protected response should be valid JSON")
	assert.eq(protected_body.message, "access granted", "should grant access")
	assert.eq(protected_body.actor_id, "queryuser", "actor_id should match username")

	return true
end

return { main = main }
