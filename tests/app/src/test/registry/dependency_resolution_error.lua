local assert = require("assert2")
local registry = require("registry")
local errors = require("errors")

local dep_id = "app.test.registry:terminal_dependency_error"

local function main()
	local snap, err = registry.snapshot()
	assert.is_nil(err, "snapshot no error")
	assert.not_nil(snap, "snapshot available")

	local changes = snap:changes()
	changes:create({
		id = dep_id,
		kind = "ns.dependency",
		data = {
			component = "wippy/terminal",
			version = ">=9999.0.0",
		},
	})

	local version, apply_err = changes:apply()
	assert.is_nil(version, "version nil on failure")
	assert.not_nil(apply_err, "apply error expected")

	local kind = apply_err:kind()
	assert.ok(kind == errors.CONFLICT or kind == errors.INTERNAL, "expected conflict or internal error kind")

	local details = apply_err:details()
	if details ~= nil then
		if details.errors ~= nil then
			assert.is_number(details.count, "details.count set")
			assert.is_string(details.summary, "details.summary set")
			assert.is_table(details.errors, "details.errors set")
			assert.ok(details.count > 0, "details.count > 0")
		end
	else
		assert.contains(tostring(apply_err), "dependency resolution failed", "error message contains resolution failure")
	end

	local entry, get_err = registry.get(dep_id)
	assert.is_nil(entry, "dependency entry not created")
	assert.not_nil(get_err, "get error expected")
	assert.eq(get_err:kind(), errors.NOT_FOUND, "entry not found after failed apply")

	return true
end

return { main = main }
