-- SPDX-License-Identifier: MPL-2.0

-- http.request_batch is a separate code path from http.get/http.request; it
-- parses per-request options inside a ForEach loop. Verify the network.select
-- gate fires there too: under a deny-network.select scope, a batch that
-- contains any request with overlay_network must fail at parse time with a
-- "not allowed" error before any request is dispatched.

local assert = require("assert2")
local security = require("security")
local funcs = require("funcs")

local function main()
	local allow, err1 = security.policy("app.test.network:allow_everything")
	assert.is_nil(err1, "fetch allow_everything: " .. tostring(err1))
	local deny, err2 = security.policy("app.test.network:deny_network_select")
	assert.is_nil(err2, "fetch deny_network_select: " .. tostring(err2))

	local scope = security.new_scope():with(allow):with(deny)
	local actor = security.new_actor("batch_test_user")

	local result, call_err = funcs.new()
	:with_actor(actor)
	:with_scope(scope)
	:call("app.test.network:batch_probe")

	assert.is_nil(call_err, "probe call failed: " .. tostring(call_err))
	assert.not_nil(result, "probe returned result table")

	assert.is_nil(result.responses, "batch must not return responses under deny")
	assert.not_nil(result.err, "batch must surface an error")
	assert.is_string(result.err, "error must be a string, got " .. type(result.err))

	local hit = string.find(result.err, "not allowed", 1, true)
		or string.find(result.err, "network", 1, true)
	assert.ok(hit, "batch error must name the denial: " .. result.err)

	return true
end

return { main = main }
