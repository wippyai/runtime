-- SPDX-License-Identifier: MPL-2.0

-- A deny-network.select scope must block explicit overlay selection at every
-- Lua edge: direct http_client.get, funcs.with_options, and
-- process.with_options. allow_everything is included so unrelated permissions
-- (funcs.call, process.context, http_client.request) stay open.

local assert = require("assert2")
local security = require("security")
local funcs = require("funcs")

local function check_denied(label, msg)
	assert.not_nil(msg, label .. ": expected error message, got nil")
	assert.is_string(msg, label .. ": error must be a string, got " .. type(msg))
	local hit = string.find(msg, "not allowed", 1, true)
		or string.find(msg, "permission", 1, true)
	assert.ok(hit, label .. ": error must indicate denial, got: " .. msg)
end

local function main()
	local allow, err1 = security.policy("app.test.network:allow_everything")
	assert.is_nil(err1, "fetch allow_everything: " .. tostring(err1))
	local deny, err2 = security.policy("app.test.network:deny_network_select")
	assert.is_nil(err2, "fetch deny_network_select: " .. tostring(err2))

	local scope = security.new_scope():with(allow):with(deny)
	local actor = security.new_actor("deny_test_user")

	-- Confirm the scope really denies network.select before we probe.
	local eval = scope:evaluate(actor, "network.select", "app.test.network:fast_socks5")
	assert.eq(eval, "deny", "scope must deny network.select outright")

	local result, call_err = funcs.new()
	:with_actor(actor)
	:with_scope(scope)
	:call("app.test.network:denied_probe", { target = "app.test.network:fast_socks5" })

	assert.is_nil(call_err, "probe call itself must succeed under allow_everything: " .. tostring(call_err))
	assert.not_nil(result, "probe returned a result table")

	assert.is_nil(result.http_resp, "httpclient: must not return a response under deny")
	check_denied("httpclient edge", result.http_err)

	assert.is_nil(result.funcs_exec, "funcs.with_options: executor must be nil under deny")
	check_denied("funcs edge", result.funcs_err)

	assert.eq(result.process_ok, false, "process.with_options must raise under deny")
	check_denied("process edge", result.process_err)

	return true
end

return { main = main }
