-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")
local hub = require("hub")
local errors = require("errors")

local module_name = "wippy/terminal"
local dep_id = "app.test.registry:terminal_dependency_update"
local hub_timeout = "20s"

local function state()
	local snap, err = registry.snapshot()
	assert.is_nil(err, "snapshot no error")
	assert.not_nil(snap, "snapshot available")

	local result, state_err = snap:state()
	assert.is_nil(state_err, "snapshot state no error")
	assert.not_nil(result, "snapshot state available")
	return result
end

local function find_module_entries()
	local entries = {}
	for _, entry in ipairs(state().entries) do
		if entry.registry.owner == module_name then
			table.insert(entries, entry)
		end
	end
	return entries
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
	assert.not_nil(second, "need at least two distinct versions to test update")
	return first, second
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

local function selected_module_version()
	local resolution = state().resolution
	assert.not_nil(resolution, "dependency resolution available")
	for _, module in ipairs(resolution.modules) do
		if module.name == module_name then
			return module.version
		end
	end
	return nil
end

local function main()
	ensure_dependency_removed()

	local version_a, version_b = module_versions()

	apply_changes(function(changes)
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
	assert.eq(entries_a[1].registry.owner, module_name, "state identifies installed entry ownership")
	local installed_version = selected_module_version()
	assert.eq(installed_version, version_a, "installed version matches first constraint")

	apply_changes(function(changes)
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
	assert.eq(entries_b[1].registry.owner, module_name, "state retains installed entry ownership after update")
	assert.eq(selected_module_version(), version_b, "updated version matches second constraint")

	apply_changes(function(changes)
		changes:delete(dep_id)
	end)
	assert.eq(#find_module_entries(), 0, "module entries removed after uninstall")

	return true
end

return { main = main }
