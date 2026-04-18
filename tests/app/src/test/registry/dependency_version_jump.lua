-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")
local hub = require("hub")
local errors = require("errors")

local module_name = "wippy/terminal"
local dep_id = "app.test.registry:terminal_dependency_version_jump"
local hub_timeout = "20s"

local function find_module_entries()
	local entries, err = registry.find({ ["meta.module"] = module_name })
	assert.is_nil(err, "registry.find no error")
	return entries or {}
end

local function first_module_version(entries)
	for i = 1, #entries do
		local entry = entries[i]
		if entry.meta ~= nil and entry.meta.module_version ~= nil then
			return entry.meta.module_version
		end
	end
	return nil
end

local function apply_changes(apply_fn)
	local snap, err = registry.snapshot()
	assert.is_nil(err, "snapshot no error")
	assert.not_nil(snap, "snapshot available")

	local changes = snap:changes()
	apply_fn(changes)

	local version, apply_err = changes:apply()
	assert.is_nil(apply_err, "apply no error")
	assert.not_nil(version, "version created")
	return version
end

local function ensure_dependency_removed()
	local entry, err = registry.get(dep_id)
	if entry ~= nil then
		apply_changes(function(changes)
			changes:delete(dep_id)
		end)
		assert.eq(#find_module_entries(), 0, "module entries removed after delete")
		return
	end
	if err ~= nil then
		assert.eq(err:kind(), errors.NOT_FOUND, "unexpected registry.get error")
	end
end

local function module_versions()
	local res, err = hub.versions.list(module_name, { page_size = 10, timeout = hub_timeout })
	assert.is_nil(err, "hub.versions.list no error")
	assert.is_table(res, "versions list response")
	local items = res.items or {}

	local first = nil
	local second = nil
	for i = 1, #items do
		local item = items[i]
		if item == nil then break end
		local v = item.version
		if v ~= nil and v ~= "" then
			if first == nil then
				first = v
			elseif v ~= first then
				second = v
				break
			end
		end
	end

	assert.not_nil(first, "at least one version available")
	assert.not_nil(second, "need at least two distinct versions to test jump")
	return first, second
end

local function main()
	ensure_dependency_removed()

	local version_a, version_b = module_versions()

	local v1 = apply_changes(function(changes)
		changes:create({
			id = dep_id,
			kind = "ns.dependency",
			data = {
				component = module_name,
				version = version_a,
			},
		})
	end)

	local entries_a = find_module_entries()
	assert.ok(#entries_a > 0, "module entries installed")
	assert.eq(first_module_version(entries_a), version_a, "installed version matches A")

	local v2 = apply_changes(function(changes)
		changes:update({
			id = dep_id,
			kind = "ns.dependency",
			data = {
				component = module_name,
				version = version_b,
			},
		})
	end)

	local entries_b = find_module_entries()
	assert.ok(#entries_b > 0, "module entries updated")
	assert.eq(first_module_version(entries_b), version_b, "installed version matches B")

	local ok, err = registry.apply_version(v1)
	assert.is_nil(err, "rollback no error")
	assert.ok(ok, "rollback ok")

	local entries_rollback = find_module_entries()
	assert.ok(#entries_rollback > 0, "module entries after rollback")
	assert.eq(first_module_version(entries_rollback), version_a, "version restored after rollback")

	local ok2, err2 = registry.apply_version(v2)
	assert.is_nil(err2, "forward apply no error")
	assert.ok(ok2, "forward apply ok")

	local entries_forward = find_module_entries()
	assert.ok(#entries_forward > 0, "module entries after forward apply")
	assert.eq(first_module_version(entries_forward), version_b, "version restored after forward apply")

	apply_changes(function(changes)
		changes:delete(dep_id)
	end)

	assert.eq(#find_module_entries(), 0, "module entries removed after uninstall")

	return true
end

return { main = main }
