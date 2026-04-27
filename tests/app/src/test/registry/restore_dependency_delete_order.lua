-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")
local funcs = require("funcs")

local base_id = "app.test.registry:restore_order_base_lib"
local wrapper_id = "app.test.registry:restore_order_wrapper_lib"
local func_id = "app.test.registry:restore_order_func"

local function base_entry(value)
	return {
		id = base_id,
		kind = "library.lua",
		meta = {
			comment = "base library for restore delete order test",
		},
		data = {
			source = "local M = {}\nfunction M.value() return '" .. value .. "' end\nreturn M",
		},
	}
end

local function wrapper_entry()
	return {
		id = wrapper_id,
		kind = "library.lua",
		meta = {
			comment = "wrapper library for restore delete order test",
			depends_on = { base_id },
		},
		data = {
			source = "local base = require('base')\nlocal M = {}\nfunction M.value() return base.value() end\nreturn M",
			imports = {
				base = base_id,
			},
		},
	}
end

local function func_entry()
	return {
		id = func_id,
		kind = "function.lua",
		meta = {
			comment = "function for restore delete order test",
			depends_on = { wrapper_id },
		},
		data = {
			source = "local wrapper = require('wrapper')\nlocal function main() return { value = wrapper.value() } end\nreturn { main = main }",
			method = "main",
			imports = {
				wrapper = wrapper_id,
			},
		},
	}
end

local function entry_exists(id)
	local entry, err = registry.get(id)
	if entry ~= nil then
		return true
	end
	if err ~= nil then
		return false
	end
	return false
end

local function new_changes()
	local snap, err = registry.snapshot()
	assert.is_nil(err, "snapshot no error")
	assert.not_nil(snap, "snapshot available")
	return snap:changes()
end

local function apply_changes(apply_fn)
	local changes = new_changes()
	local count = apply_fn(changes) or 0
	assert.ok(count > 0, "apply_changes requires at least one operation")

	local version, apply_err = changes:apply()
	assert.is_nil(apply_err, "apply no error")
	assert.not_nil(version, "version created")
	return version
end

local function delete_leftovers()
	local changes = new_changes()
	local count = 0
	if entry_exists(func_id) then
		changes:delete(func_id)
		count = count + 1
	end
	if entry_exists(wrapper_id) then
		changes:delete(wrapper_id)
		count = count + 1
	end
	if entry_exists(base_id) then
		changes:delete(base_id)
		count = count + 1
	end
	if count > 0 then
		local _, apply_err = changes:apply()
		assert.is_nil(apply_err, "leftover delete no error")
	end
end

local function wait_call(expected)
	local result, call_err = funcs.call(func_id)
	assert.is_nil(call_err, "dependent function call no error")
	assert.not_nil(result, "dependent function returned result")
	if result then
		assert.eq(result.value, expected, "dependent function used imported libraries")
	end
end

local function wait_async_call(expected)
	local future, async_err = funcs.async(func_id)
	assert.is_nil(async_err, "dependent async function call no error")
	assert.not_nil(future, "dependent async function returned future")

	local payload, ok = future:response():receive()
	assert.eq(ok, true, "dependent async response received")
	assert.not_nil(payload, "dependent async returned payload")
	if payload then
		assert.eq(payload:data().value, expected, "dependent async function used imported libraries")
	end
end

local function assert_absent(id)
	local entry, err = registry.get(id)
	assert.is_nil(entry, id .. " should not exist after version rewind")
	assert.not_nil(err, id .. " lookup should return not found")
end

local function create_chain(value)
	return apply_changes(function(changes)
		changes:create(base_entry(value))
		changes:create(wrapper_entry())
		changes:create(func_entry())
		return 3
	end)
end

local function update_base(value)
	return apply_changes(function(changes)
		changes:update(base_entry(value))
		return 1
	end)
end

local function main()
	delete_leftovers()

	local original_version, version_err = registry.current_version()
	assert.is_nil(version_err, "current_version no error")
	assert.not_nil(original_version, "have original version")
	if original_version == nil then
		return false
	end
	local original_id = original_version:id()

	local installed_version = apply_changes(function(changes)
		changes:create(base_entry("base-v1"))
		changes:create(wrapper_entry())
		changes:create(func_entry())
		return 3
	end)
	assert.not_nil(installed_version, "installed version created")

	wait_call("base-v1")
	wait_async_call("base-v1")

	local rollback_ok, rollback_err = registry.apply_version(original_version)
	assert.is_nil(rollback_err, "rollback deletes dependents before dependencies")
	assert.ok(rollback_ok, "rollback succeeded")

	local after_rollback, after_err = registry.current_version()
	assert.is_nil(after_err, "current_version after rollback no error")
	assert.not_nil(after_rollback, "have version after rollback")
	if after_rollback == nil then
		return false
	end
	assert.eq(after_rollback:id(), original_id, "version restored to original")
	assert_absent(func_id)
	assert_absent(wrapper_id)
	assert_absent(base_id)

	local removed_result, removed_err = funcs.call(func_id)
	assert.is_nil(removed_result, "removed function returns nil")
	assert.not_nil(removed_err, "removed function is no longer callable")

	local forward_ok, forward_err = registry.apply_version(installed_version)
	assert.is_nil(forward_err, "forward apply no error")
	assert.ok(forward_ok, "forward apply succeeded")
	wait_call("base-v1")
	wait_async_call("base-v1")

	local final_ok, final_err = registry.apply_version(original_version)
	assert.is_nil(final_err, "final cleanup rollback no error")
	assert.ok(final_ok, "final cleanup rollback succeeded")
	assert_absent(func_id)
	assert_absent(wrapper_id)
	assert_absent(base_id)

	local rapid_v1 = create_chain("rapid-v1")
	wait_call("rapid-v1")
	wait_async_call("rapid-v1")

	local rapid_v2 = update_base("rapid-v2")
	wait_call("rapid-v2")
	wait_async_call("rapid-v2")

	local rapid_v3 = update_base("rapid-v3")
	wait_call("rapid-v3")
	wait_async_call("rapid-v3")

	local rapid_rollback_ok, rapid_rollback_err = registry.apply_version(original_version)
	assert.is_nil(rapid_rollback_err, "rapid rollback to baseline no error")
	assert.ok(rapid_rollback_ok, "rapid rollback to baseline succeeded")
	assert_absent(func_id)
	assert_absent(wrapper_id)
	assert_absent(base_id)

	local rapid_forward_ok, rapid_forward_err = registry.apply_version(rapid_v3)
	assert.is_nil(rapid_forward_err, "rapid forward to latest no error")
	assert.ok(rapid_forward_ok, "rapid forward to latest succeeded")
	wait_call("rapid-v3")
	wait_async_call("rapid-v3")

	local rapid_back_one_ok, rapid_back_one_err = registry.apply_version(rapid_v1)
	assert.is_nil(rapid_back_one_err, "rapid rollback to first created version no error")
	assert.ok(rapid_back_one_ok, "rapid rollback to first created version succeeded")
	wait_call("rapid-v1")
	wait_async_call("rapid-v1")

	local rapid_forward_again_ok, rapid_forward_again_err = registry.apply_version(rapid_v2)
	assert.is_nil(rapid_forward_again_err, "rapid forward to middle version no error")
	assert.ok(rapid_forward_again_ok, "rapid forward to middle version succeeded")
	wait_call("rapid-v2")
	wait_async_call("rapid-v2")

	local rapid_cleanup_ok, rapid_cleanup_err = registry.apply_version(original_version)
	assert.is_nil(rapid_cleanup_err, "rapid cleanup rollback no error")
	assert.ok(rapid_cleanup_ok, "rapid cleanup rollback succeeded")
	assert_absent(func_id)
	assert_absent(wrapper_id)
	assert_absent(base_id)

	return true
end

return { main = main }
