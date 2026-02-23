-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function main()
-- get all versions
	local versions, err = registry.versions()
	assert.is_nil(err, "versions no error")
	assert.not_nil(versions, "versions returned")
	assert.ok(#versions > 0, "has versions")

	-- get current version
	local current, curr_err = registry.current_version()
	assert.is_nil(curr_err, "current_version no error")
	assert.not_nil(current, "current version returned")

	-- version:id() - can be 0 at initialization (v0)
	local current_id = current:id()
	assert.eq(type(current_id), "number", "version id is number")
	assert.ok(current_id >= 0, "version id is non-negative")

	-- version:string()
	local str = current:string()
	assert.eq(type(str), "string", "version string is string")
	assert.ok(#str > 0, "version string not empty")

	-- test tostring (metamethod)
	local ts = tostring(current)
	assert.ok(string.find(ts, "Version", 1, true), "tostring contains Version")

	-- test version navigation if we have multiple versions
	if #versions > 1 then
	-- first version should have no previous
		local first = versions[1]
		assert.not_nil(first, "should have first version")
		local prev = first:previous()
		assert.is_nil(prev, "first version has no previous")

		-- first version should have next
		local next_v = first:next()
		assert.not_nil(next_v, "first version has next")

		-- next version's previous should be first
		local back = next_v:previous()
		assert.not_nil(back, "can go back from next")
		assert.eq(back:id(), first:id(), "back matches first")

		-- test chain navigation
		local v = versions[1]
		assert.not_nil(v, "should have version for chain navigation")
		local count = 1
		while true do
			local n = v:next()
			if n == nil then
				break
			end
			v = n
			count = count + 1
		end
		assert.eq(count, #versions, "navigating next covers all versions")

		-- navigate backwards
		count = 1
		while true do
			local p = v:previous()
			if p == nil then
				break
			end
			v = p
			count = count + 1
		end
		assert.eq(count, #versions, "navigating previous covers all versions")
	else
	-- single version case (v0) - no next/previous
		local prev = current:previous()
		assert.is_nil(prev, "single version has no previous")

		local next_v = current:next()
		assert.is_nil(next_v, "single version has no next")
	end

	-- test history:get_version
	local hist, hist_err = registry.history()
	assert.is_nil(hist_err, "history no error")
	assert.not_nil(hist, "history returned")

	local fetched, fetch_err = hist:get_version(current_id)
	assert.is_nil(fetch_err, "get_version no error")
	assert.not_nil(fetched, "get_version returned version")
	assert.eq(fetched:id(), current_id, "fetched version id matches")

	-- test get_version with invalid id
	local bad, bad_err = hist:get_version(999999)
	assert.is_nil(bad, "invalid id returns nil")
	assert.not_nil(bad_err, "invalid id returns error")

	return true
end

return { main = main }
