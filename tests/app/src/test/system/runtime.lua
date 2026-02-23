-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local system = require("system")

	-- Test runtime.goroutines
	local count, err = system.runtime.goroutines()
	assert.is_nil(err, "goroutines should not error")
	assert.not_nil(count, "goroutines returned")
	assert.eq(type(count), "number", "goroutines is number")
	assert.ok(count > 0, "goroutines > 0")

	-- Test runtime.cpu_count
	local cpus, err = system.runtime.cpu_count()
	assert.is_nil(err, "cpu_count should not error")
	assert.not_nil(cpus, "cpu_count returned")
	assert.eq(type(cpus), "number", "cpu_count is number")
	assert.ok(cpus > 0, "cpu_count > 0")

	-- Test runtime.max_procs (getter)
	local procs, err = system.runtime.max_procs()
	assert.is_nil(err, "max_procs get should not error")
	assert.not_nil(procs, "max_procs returned")
	assert.eq(type(procs), "number", "max_procs is number")
	assert.ok(procs > 0, "max_procs > 0")

	-- Test runtime.max_procs (setter)
	local orig = procs
	local target = orig == 2 and 3 or 2
	local old, err = system.runtime.max_procs(target)
	assert.is_nil(err, "max_procs set should not error")
	assert.eq(old, orig, "max_procs returns old value")

	-- Verify change
	local new, err = system.runtime.max_procs()
	assert.is_nil(err, "max_procs after set should not error")
	assert.eq(new, target, "max_procs changed")

	-- Restore
	system.runtime.max_procs(orig)

	return true
end

return { main = main }
