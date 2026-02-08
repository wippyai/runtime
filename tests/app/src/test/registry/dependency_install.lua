local assert = require("assert2")
local registry = require("registry")
local fs = require("fs")
local errors = require("errors")

local module_name = "wippy/terminal"
local dep_id = "app.test.registry:terminal_dependency"
local default_version = ">=0.0.0"

local function err_contains(err, substr)
	if err == nil then
		return false
	end
	local msg = err
	if type(err) == "table" and err.message then
		msg = err.message
	end
	return tostring(msg):find(substr, 1, true) ~= nil
end

local function find_module_entries()
	local entries, err = registry.find({ ["meta.module"] = module_name })
	assert.is_nil(err, "registry.find no error")
	return entries or {}
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

local function find_wapp_file(workspace)
	local vendor_dir = "/.wippy/vendor/wippy"
	local iter, state = workspace:readdir(vendor_dir)
	if iter == nil then
		return nil
	end
	for entry in iter, state do
		if entry.type == fs.type.FILE then
			local name = entry.name or ""
			if name:match("^terminal%-.*%.wapp$") then
				return vendor_dir .. "/" .. name
			end
		end
	end
	return nil
end

local function main()
	ensure_dependency_removed()

	local initial_entries = find_module_entries()
	local initial_count = #initial_entries

	apply_changes(function(changes)
		changes:create({
			id = dep_id,
			kind = "ns.dependency",
			data = {
				component = module_name,
				version = default_version,
			},
		})
	end)

	assert.ok(#find_module_entries() > initial_count, "module entries installed")

	local entries = find_module_entries()
	assert.ok(#entries > 0, "module entries present")
	assert.not_nil(entries[1].id, "module entry has id")

	local workspace, fs_err = fs.get("app:workspace")
	if workspace ~= nil then
		local wapp_path = find_wapp_file(workspace)
		assert.not_nil(wapp_path, "module wapp exists on disk")

		local exists, exists_err = workspace:exists(wapp_path)
		assert.is_nil(exists_err, "exists no error")
		assert.ok(exists, "module wapp exists on disk")
	else
		assert.not_nil(fs_err, "workspace fs error returned")
		assert.eq(fs_err:kind(), errors.NOT_FOUND, "workspace fs not configured")
	end

	apply_changes(function(changes)
		changes:delete(dep_id)
	end)

	assert.eq(#find_module_entries(), initial_count, "module entries removed after uninstall")

	return true
end

return { main = main }
