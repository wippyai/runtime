-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local mylib = require("mylib")

local function main(): boolean
	local srv = mylib.create_server("localhost", 8080, {"api"})
	local srv_ok, srv_err = mylib.Server:is(srv)
	assert.not_nil(srv_ok, "Server from library should validate")
	assert.is_nil(srv_err, "Server from library should have nil error")

	local srv_bad_host, srv_bad_host_err = mylib.Server:is({host = "", port = 8080})
	assert.is_nil(srv_bad_host, "Server with empty host should fail")
	assert.not_nil(srv_bad_host_err, "Server with empty host should return error")
	assert.error_contains(srv_bad_host_err, "length", "Host min_len should mention length")

	local srv_bad_port, srv_bad_port_err = mylib.Server:is({host = "ok", port = 70000})
	assert.is_nil(srv_bad_port, "Server with port above max should fail")
	assert.not_nil(srv_bad_port_err, "Server with port above max should return error")
	assert.error_contains(srv_bad_port_err, "maximum", "Port max should mention maximum")

	local email_ok, email_err = mylib.Email:is("a@b.com")
	assert.not_nil(email_ok, "Email pattern should pass")
	assert.is_nil(email_err, "Email pattern should have nil error")

	local email_bad, email_bad_err = mylib.Email:is("invalid")
	assert.is_nil(email_bad, "Email pattern mismatch should fail")
	assert.not_nil(email_bad_err, "Email pattern mismatch should return error")
	assert.error_contains(email_bad_err, "pattern", "Email error should mention pattern")

	local user = mylib.create_user("u1", "u1@example.com", {"admin"})
	local user_ok, user_err = mylib.User:is(user)
	assert.not_nil(user_ok, "User from library should validate")
	assert.is_nil(user_err, "User from library should have nil error")

	local user_bad, user_bad_err = mylib.User:is({id = "u1", email = "bad", roles = {}})
	assert.is_nil(user_bad, "User with invalid email/roles should fail")
	assert.not_nil(user_bad_err, "User with invalid email/roles should return error")

	return true
end

return { main = main }
