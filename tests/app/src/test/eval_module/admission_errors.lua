-- SPDX-License-Identifier: MPL-2.0

-- Test: eval admission rejects invalid options and policies.
local assert = require("assert")

local function expect_error(result, err, fragment, message)
	assert.is_nil(result, message .. " returns nil")
	assert.contains(tostring(err), fragment, message)
end

local function main()
	local source = "return { main = function() return 1 end }"

	local r, err = eval.compile(source, { unknown_key = true })
	expect_error(r, err, "unknown or non-string field", "unknown option")

	r, err = eval.compile(source, { compile = ("jit" :: any) })
	expect_error(r, err, 'unknown eval compile mode "jit"', "jit is not a compile mode")

	r, err = eval.compile(source, { commands = { "upgrade" } })
	expect_error(r, err, "eval policy unsupported", "upgrade command")

	r, err = eval.compile(source, { commands = { "spawn" } })
	expect_error(r, err, "eval policy unsupported", "spawn without max_children")

	r, err = eval.compile(source, { allow_classes = { "storage" } })
	expect_error(r, err, "eval policy unsupported", "denied class")

	r, err = eval.compile(source, { send_targets = { process.pid() } })
	expect_error(r, err, "eval policy unsupported", "send_targets without explicit mode")

	r, err = eval.compile(source, { modules = { "sql" } })
	expect_error(r, err, "sql", "storage module")

	for _, name in ipairs({ "eval", "exec", "env", "system", "registry", "fs", "store", "http_client", "websocket" }) do
		r, err = eval.compile(source, { modules = { name } })
		expect_error(r, err, name, "module " .. name .. " is not admissible")
	end
	for _, class in ipairs({ "process", "network" }) do
		r, err = eval.compile(source, { allow_classes = { class } })
		expect_error(r, err, "eval policy unsupported", "class " .. class .. " cannot be allowed")
	end

	r, err = eval.compile("", {})
	expect_error(r, err, "eval source required", "empty source")

	r, err = eval.compile("return {", {})
	expect_error(r, err, "", "syntax error")

	r, err = eval.spawn(source, { detached = true })
	expect_error(r, err, "eval detached spawn denied", "detached without allow_detached")

	r, err = eval.spawn(source, { limits = { memory_bytes = 1024 }, monitor_only = true })
	assert.is_nil(err, "memory limits are accepted")

	r, err = eval.compile(source, { limits = { tick_budget = 0 } })
	expect_error(r, err, "greater than zero", "zero tick_budget")
	return true
end

return { main = main }
