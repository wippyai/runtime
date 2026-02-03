local assert = require("assert2")
local registry = require("registry")
local funcs = require("funcs")
local time = require("time")

local function main()
-- get current version before changes
	local current_version, _ = registry.current_version()
	assert.not_nil(current_version, "have current version")
	local original_id = current_version:id()

	-- create a snapshot for building changes
	local snap, _ = registry.snapshot()
	assert.not_nil(snap, "have snapshot")

	-- define a new function entry (data contains FunctionConfig fields)
	local func_id = "app.test.registry:dynamic_test_func"
	local func_entry = {
		id = func_id,
		kind = "function.lua",
		meta = {
			type = "test",
			suite = "registry",
			description = "dynamically created function for version test"
		},
		data = {
			source = "local function main() return { created = true } end\nreturn { main = main }",
			method = "main"
		}
	}

	-- create changeset and add the function
	local changes = snap:changes()
	changes:create(func_entry)

	-- apply the changes
	local new_version, apply_err = changes:apply()
	assert.is_nil(apply_err, "apply no error")
	assert.not_nil(new_version, "new version created")

	local new_id = new_version:id()
	assert.ok(new_id > original_id, "new version id is higher")

	-- wait for function manager to create pool for the new function
	-- use polling since event propagation timing is non-deterministic
	local result = nil
	local call_err = nil
	for _ = 1, 20 do
		result, call_err = funcs.call(func_id)
		if call_err == nil then
			break
		end
		time.sleep("50ms")
	end
	assert.is_nil(call_err, "call dynamic func no error")
	assert.not_nil(result, "call returns result")
	if result then
		assert.eq(result.created, true, "function returned expected value")
	end

	-- now rollback to original version
	local rollback_ok, rollback_err = registry.apply_version(current_version)
	assert.is_nil(rollback_err, "rollback no error")
	assert.ok(rollback_ok, "rollback succeeded")

	-- verify current version is back to original
	local after_version, _ = registry.current_version()
	assert.not_nil(after_version, "have after version")
	assert.eq(after_version:id(), original_id, "version rolled back")

	-- function should no longer be callable
	local fail_result, fail_err = funcs.call(func_id)
	assert.is_nil(fail_result, "rolled back func returns nil")
	assert.not_nil(fail_err, "rolled back func returns error")

	return true
end

return { main = main }
