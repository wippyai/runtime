-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local funcs = require("funcs")
local registry = require("registry")

local function entry_for(id, value)
	return {
		id = id,
		kind = "function.lua",
		meta = {
			comment = "delete/recreate same-id cache invalidation regression",
		},
		data = {
			source = "return { main = function() return { value = \"" .. value .. "\" } end }",
			method = "main",
		},
	}
end

local function main()
	local original_version, version_err = registry.current_version()
	assert.is_nil(version_err, "current version no error")
	assert.not_nil(original_version, "have original version")

	local func_id = "app.test.registry:delete_recreate_same_id_target"

	local snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	local create_changes = snap:changes()
	create_changes:create(entry_for(func_id, "bad"))
	local created_version, create_err = create_changes:apply()
	assert.is_nil(create_err, "create first function version")
	assert.not_nil(created_version, "created version returned")

	local first, first_err = funcs.call(func_id)
	assert.is_nil(first_err, "first call no error")
	assert.eq(first.value, "bad", "first body is active")

	local rollback_ok, rollback_err = registry.apply_version(original_version)
	assert.is_nil(rollback_err, "rollback to original version")
	assert.ok(rollback_ok, "rollback succeeded")

	local snap2, snap2_err = registry.snapshot()
	assert.is_nil(snap2_err, "second snapshot no error")
	local recreate_changes = snap2:changes()
	recreate_changes:create(entry_for(func_id, "good"))
	local recreated_version, recreate_err = recreate_changes:apply()
	assert.is_nil(recreate_err, "recreate same id")
	assert.not_nil(recreated_version, "recreated version returned")

	local second, second_err = funcs.call(func_id)
	assert.is_nil(second_err, "second call no error")
	assert.eq(second.value, "good", "recreated same-id function uses new source")

	local final_ok, final_err = registry.apply_version(original_version)
	assert.is_nil(final_err, "restore original version")
	assert.ok(final_ok, "restore original succeeded")

	return true
end

return { main = main }
