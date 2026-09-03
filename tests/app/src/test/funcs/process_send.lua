local assert = require("assert2")
local funcs = require("funcs")
local security = require("security")
local time = require("time")

local function receive(inbox, want)
	local timeout = time.after("2s")
	local result = channel.select {
		inbox:case_receive(),
		timeout:case_receive(),
	}
	assert.eq(result.channel, inbox, "message received")
	local message = result.value
	assert.eq(message:topic(), "mcp.command", "message topic")
	assert.eq(message:payload():data().value, want, "message payload")
end

local function main()
	local name = "funcs_send_" .. tostring(process.pid())
	local registered, register_err = process.registry.register(name)
	assert.is_nil(register_err, "registry registration has no error")
	assert.ok(registered, "registry registration succeeds")

	local inbox = process.inbox()
	local policy, policy_err = security.policy("app.test.security:allow_all")
	assert.is_nil(policy_err, "allow policy lookup has no error")
	local scope = security.new_scope():with(policy)
	local actor = security.new_actor("funcs_process_send")
	local exec = funcs.new():with_actor(actor):with_scope(scope)

	local sent, statement_err = exec:call("app.test.funcs:send_statement", name, "statement")
	assert.is_nil(statement_err, "statement send has no error: " .. tostring(statement_err))
	assert.ok(sent, "statement send succeeds")
	receive(inbox, "statement")

	sent, statement_err = exec:call("app.test.funcs:send_pressure", name, "pressure")
	assert.is_nil(statement_err, "pressure send has no error: " .. tostring(statement_err))
	assert.ok(sent, "pressure send succeeds")
	receive(inbox, "pressure")

	sent, statement_err = exec:call("app.test.funcs:send_tail", name, "tail")
	assert.is_nil(statement_err, "tail send has no error: " .. tostring(statement_err))
	assert.ok(sent, "tail send succeeds")
	receive(inbox, "tail")

	local unregistered = process.registry.unregister(name)
	assert.ok(unregistered, "registry removal succeeds")
	return true
end

return { main = main }
