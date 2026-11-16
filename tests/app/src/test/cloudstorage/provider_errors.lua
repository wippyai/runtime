-- SPDX-License-Identifier: MPL-2.0

-- Test: provider failures are classified for every operation that reaches the provider
local assert = require("assert_primitives")

local function storage(name)
	local cloudstorage = require("cloudstorage")
	local s, err = cloudstorage.get("app.test.cloudstorage:" .. name)
	assert.is_nil(err, "should get storage " .. name)
	return s
end

local function read_operations(s)
	return {
		list_objects = function() return s:list_objects() end,
		list_object_versions = function() return s:list_objects({ include_versions = true }) end,
		head_object = function() return s:head_object("provider-errors.txt") end,
	}
end

local function operations(s)
	return {
		list_objects = function() return s:list_objects() end,
		list_object_versions = function() return s:list_objects({ include_versions = true }) end,
		head_object = function() return s:head_object("provider-errors.txt") end,
		upload_object = function() return s:upload_object("provider-errors.txt", "content") end,
		delete_objects = function() return s:delete_objects({ "provider-errors.txt" }) end,
		create_multipart_upload = function() return s:create_multipart_upload("provider-errors.bin") end,
	}
end

local function expect(ops, kind, retryable)
	for name, op in pairs(ops) do
		local result, err = op()
		assert.is_nil(result, name .. " should fail")
		assert.not_nil(err, name .. " should return an error")
		assert.eq(err:kind(), kind, name .. " error kind")
		assert.eq(err:retryable(), retryable, name .. " retryable")
	end
end

local function main()
	local missing = storage("minio_missing_bucket")
	local _, err = missing:list_objects()
	assert.eq(err:message(), "not_found", "missing bucket keeps the not_found message")
	assert.eq(err:details().code, "NoSuchBucket", "missing bucket provider code")
	-- Some S3-compatible stores create a missing bucket on write, so only reads are portable here.
	expect(read_operations(missing), errors.NOT_FOUND, false)
	missing:release()

	local rejected = storage("minio_rejected")
	expect(operations(rejected), errors.PERMISSION_DENIED, false)
	rejected:release()

	local unreachable = storage("minio_unreachable")
	-- A host that does not resolve is unavailable; the SDK retry policy does not retry it.
	expect(operations(unreachable), errors.UNAVAILABLE, false)
	unreachable:release()

	return true
end

return { main = main }
