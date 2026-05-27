-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local http = require("http_client")
	local json = require("json")

	local login_res, err = http.post("http://localhost:8085/auth/login", {
		body = json.encode({ username = "testuser" }),
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

	local protected_res, err = http.get("http://localhost:8085/auth/protected", {
		headers = { ["Authorization"] = "Bearer " .. token }
	})
	if err then
		return false, "protected request error: " .. tostring(err)
	end

	if protected_res.status_code ~= 200 then
		return false, "expected 200, got " .. protected_res.status_code .. ": " .. (protected_res.body or "")
	end

	assert.not_nil(protected_res.body, "protected response body present")
	local protected_body = json.decode(tostring(protected_res.body))
	if not protected_body then
		return false, "invalid JSON response, status=" .. protected_res.status_code .. ", body=[" .. (protected_res.body or "nil") .. "]"
	end
	assert.eq(protected_body.message, "access granted", "should grant access")
	assert.eq(protected_body.actor_id, "testuser", "actor_id should match username")

	return true
end

return { main = main }
