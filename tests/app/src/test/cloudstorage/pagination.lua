-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage list_objects pagination via continuation_token.
-- Uploads N objects, walks the listing in pages of max_keys, and asserts
-- every key is observed exactly once across pages.
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	local prefix = "pagination-test/"
	local keys = {
		prefix .. "obj-01.txt",
		prefix .. "obj-02.txt",
		prefix .. "obj-03.txt",
		prefix .. "obj-04.txt",
		prefix .. "obj-05.txt",
		prefix .. "obj-06.txt",
		prefix .. "obj-07.txt",
	}
	local total = #keys
	local expected = {}
	for i, key in ipairs(keys) do
		local _, uerr = storage:upload_object(key, "body-" .. i)
		assert.is_nil(uerr, "upload should not error for " .. key)
		expected[key] = true
	end

	-- Walk the listing in pages of 2.
	-- continuation_token is initialized to the empty string and refreshed each
	-- page; the empty string is treated as "no token" by the underlying driver.
	local seen = {}
	local pages = 0
	local token = ""
	local guard = 0
	repeat
		pages = pages + 1
		guard = guard + 1
		assert.eq(guard <= total + 5, true, "pagination loop should terminate")

		local result, lerr = storage:list_objects({
			prefix = prefix,
			max_keys = 2,
			continuation_token = token,
		})
		assert.is_nil(lerr, "page list should not error")
		assert.not_nil(result, "page should return a result")
		assert.eq(#result.objects <= 2, true, "page should respect max_keys")

		for _, obj in ipairs(result.objects) do
			assert.eq(seen[obj.key], nil, "each key should appear at most once across pages: " .. obj.key)
			seen[obj.key] = true
		end

		if not result.is_truncated then
			break
		end
		token = result.next_continuation_token or ""
		assert.eq(token ~= "", true,
			"truncated page should provide a non-empty continuation_token")
	until false

	-- Every uploaded key should have been observed exactly once.
	local seen_count = 0
	for k in pairs(seen) do
		assert.eq(expected[k], true, "observed unexpected key: " .. k)
		seen_count = seen_count + 1
	end
	assert.eq(seen_count, total, "all uploaded keys should be observed")
	-- And we should have used multiple pages.
	assert.eq(pages >= math.ceil(total / 2), true, "pagination should span multiple pages")

	-- Cleanup.
	storage:delete_objects(keys)
	storage:release()

	return true
end

return { main = main }
