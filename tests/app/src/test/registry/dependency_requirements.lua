-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")
local http = require("http_client")
local json = require("json")
local time = require("time")
local errors = require("errors")

local module_name = "wippy/dummy"
local dep_id = "app.test.registry:views_dependency_requirements"
local param_name = "router"
local router_primary = "app:api.public"
local router_secondary = "app:api.ws"

local function find_module_entries()
	local entries, err = registry.find({ ["meta.module"] = module_name })
	assert.is_nil(err, "registry.find no error")
	return entries or {}
end

local function find_requirements()
	local entries, err = registry.find({
		[".kind"] = "ns.requirement",
		["meta.module"] = module_name,
	})
	assert.is_nil(err, "registry.find no error")
	return entries or {}
end

local function find_endpoints()
	local entries, err = registry.find({
		[".kind"] = "http.endpoint",
		["meta.module"] = module_name,
	})
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
		local ok = wait_for(function()
			return #find_module_entries() == 0
		end, 60000, 250)
		assert.ok(ok, "module entries removed after delete")
		return
	end
	if err ~= nil then
		assert.eq(err:kind(), errors.NOT_FOUND, "unexpected registry.get error")
	end
end

local function parse_path(path)
	local append = false
	local p = tostring(path or "")
	if p:match("%+=$") then
		append = true
		p = p:gsub("%+=$", "")
	end
	p = p:gsub("^%s+", ""):gsub("%s+$", "")
	p = p:gsub("^%.", "")
	if p == "" then
		return "data", {}, append
	end
	local parts = {}
	for seg in p:gmatch("[^%.]+") do
		parts[#parts + 1] = seg
	end
	local target = parts[1]
	if target == "data" or target == "meta" then
		local rest = {}
		for i = 2, #parts do
			rest[#rest + 1] = parts[i]
		end
		return target, rest, append
	end
	return "data", parts, append
end

local function get_at_path(entry, path)
	local target, parts, append = parse_path(path)
	local cur = nil
	if target == "meta" then
		cur = entry.meta
	else
		cur = entry.data
	end
	for i = 1, #parts do
		if type(cur) ~= "table" then
			return nil, append
		end
		cur = cur[parts[i]]
	end
	return cur, append
end

local function array_contains(arr, value)
	if type(arr) ~= "table" then
		return false
	end
	for i = 1, #arr do
		if arr[i] == value then
			return true
		end
	end
	return false
end

local function main()
	ensure_dependency_removed()

	local install_version = apply_changes(function(changes)
		changes:create({
			id = dep_id,
			kind = "ns.dependency",
			data = {
				component = module_name,
				version = "0.1.2",
				parameters = {
					{ name = param_name, value = router_primary },
				},
			},
		})
	end)

	local module_entries = find_module_entries()
	local module_count = #module_entries
	assert.ok(module_count > 0, "module entries counted")

	local reqs = find_requirements()
	assert.ok(#reqs > 0, "module requirements present")

	local endpoints = find_endpoints()
	assert.ok(#endpoints > 0, "module endpoints present")
	for i = 1, #endpoints do
		local endpoint = endpoints[i]
		local meta = endpoint.meta or {}
		assert.eq(meta.router, router_primary, "endpoint router is public")
	end

	local matched = 0
	for i = 1, #reqs do
		local req = reqs[i]
		local id = registry.parse_id(tostring(req.id))
		if id.name == param_name then
			matched = matched + 1
			local targets = (req.data or {}).targets or {}
			assert.ok(#targets > 0, "requirement has targets")
			for t = 1, #targets do
				local target = targets[t]
				assert.not_nil(target, "target present")
				local target_entry, get_err = registry.get(tostring(target.entry))
				assert.is_nil(get_err, "target entry exists")
				assert.not_nil(target_entry, "target entry found")

				local value, append = get_at_path(target_entry, target.path)
				if append then
					assert.ok(array_contains(value, router_primary), "target includes parameter value")
				else
					assert.eq(value, router_primary, "target value matches parameter")
				end
			end
		end
	end

	assert.ok(matched > 0, "matched requirement by name")

	local resp, http_err = http.get("http://localhost:8085/dummy/ping")
	assert.is_nil(http_err, "http get /dummy/ping no error")
	assert.not_nil(resp, "http response returned")
	assert.eq(resp.status_code, 200, "http /dummy/ping status 200")
	if resp.body ~= nil and resp.body ~= "" then
		local decoded = json.decode(tostring(resp.body))
		assert.is_table(decoded, "http /dummy/ping response json")
		assert.eq(decoded.message, "pong", "dummy ping message")
		assert.eq(decoded.module, "wippy/dummy", "dummy ping module")
	end

	local update_version = apply_changes(function(changes)
		changes:update({
			id = dep_id,
			kind = "ns.dependency",
			data = {
				component = module_name,
				version = "0.1.2",
				parameters = {
					{ name = param_name, value = router_secondary },
				},
			},
		})
	end)

	local updated_endpoints = find_endpoints()
	assert.ok(#updated_endpoints > 0, "endpoints present after update")
	for i = 1, #updated_endpoints do
		local endpoint = updated_endpoints[i]
		local meta = endpoint.meta or {}
		assert.eq(meta.router, router_secondary, "endpoint router updated")
	end

	local updated_count = #find_module_entries()
	assert.eq(updated_count, module_count, "module entries count stable after update")

	local rollback_ok, rollback_err = registry.apply_version(install_version)
	assert.is_nil(rollback_err, "rollback no error")
	assert.ok(rollback_ok, "rollback succeeded")

	local rolled_back_endpoints = find_endpoints()
	assert.ok(#rolled_back_endpoints > 0, "endpoints present after rollback")
	for i = 1, #rolled_back_endpoints do
		local endpoint = rolled_back_endpoints[i]
		local meta = endpoint.meta or {}
		assert.eq(meta.router, router_primary, "endpoint router restored")
	end

	local forward_ok, forward_err = registry.apply_version(update_version)
	assert.is_nil(forward_err, "forward apply no error")
	assert.ok(forward_ok, "forward apply succeeded")

	local forwarded_endpoints = find_endpoints()
	assert.ok(#forwarded_endpoints > 0, "endpoints present after forward apply")
	for i = 1, #forwarded_endpoints do
		local endpoint = forwarded_endpoints[i]
		local meta = endpoint.meta or {}
		assert.eq(meta.router, router_secondary, "endpoint router restored after forward apply")
	end

	apply_changes(function(changes)
		changes:delete(dep_id)
	end)

	assert.eq(#find_module_entries(), 0, "module entries removed after uninstall")
	assert.eq(#find_endpoints(), 0, "module endpoints removed after uninstall")

	return true
end

return { main = main }
