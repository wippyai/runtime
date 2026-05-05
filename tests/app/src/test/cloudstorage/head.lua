-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage head_object, upload options, and conditional ops
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	local key = "head-test/object.txt"
	local body = "Hello from head_object"

	-- Upload with rich options: content_type, cache_control, content_disposition, custom metadata
	local ok, err1 = storage:upload_object(key, body, {
		content_type = "text/plain; charset=utf-8",
		cache_control = "max-age=3600",
		content_disposition = "inline",
		metadata = { env = "staging", owner = "tests" },
	})
	assert.is_nil(err1, "upload with options should not error")
	assert.eq(ok, true, "upload should return true")

	-- head_object returns full metadata, including user-defined metadata.
	local head, herr = storage:head_object(key)
	assert.is_nil(herr, "head_object should not error")
	assert.not_nil(head, "head_object should return a result")
	assert.eq(head.size, #body, "size should match body length")
	assert.eq(type(head.etag), "string", "etag should be a string")
	assert.eq(head.etag ~= "", true, "etag should not be empty")
	assert.eq(head.content_type, "text/plain; charset=utf-8", "content_type should round-trip")
	assert.eq(head.cache_control, "max-age=3600", "cache_control should round-trip")
	assert.eq(head.content_disposition, "inline", "content_disposition should round-trip")
	assert.eq(type(head.last_modified), "number", "last_modified should be a unix timestamp")
	assert.eq(head.last_modified > 0, true, "last_modified should be > 0")
	assert.not_nil(head.metadata, "should have metadata table")
	-- AWS lowercases user metadata keys; values pass through as-is.
	assert.eq(head.metadata.env, "staging", "user metadata env should round-trip")
	assert.eq(head.metadata.owner, "tests", "user metadata owner should round-trip")

	-- Conditional download: if_none_match with the current etag should yield precondition_failed.
	local fs = require("fs")
	local vol, _ = fs.get("app:temp")
	local file, _ = vol:open("/cloudstorage_head_dl.txt", "w")
	local _, derr = storage:download_object(key, file, { if_none_match = head.etag })
	file:close()
	vol:remove("/cloudstorage_head_dl.txt")
	assert.not_nil(derr, "download with matching if_none_match should fail")
	assert.eq(derr:kind(), "Conflict", "precondition error should map to Conflict kind")

	-- Conditional download: if_match with current etag should succeed.
	local file2, _ = vol:open("/cloudstorage_head_dl_ok.txt", "w")
	local dok, derr2 = storage:download_object(key, file2, { if_match = head.etag })
	file2:close()
	assert.is_nil(derr2, "download with matching if_match should succeed")
	assert.eq(dok, true, "download with matching if_match should return true")
	local got = vol:readfile("/cloudstorage_head_dl_ok.txt")
	assert.eq(got, body, "downloaded content should match original")
	vol:remove("/cloudstorage_head_dl_ok.txt")

	-- Conditional upload: if_none_match = "*" against an existing object should fail.
	local _, uerr = storage:upload_object(key, "should not overwrite", {
		if_none_match = "*",
	})
	assert.not_nil(uerr, "upload with if_none_match=* should fail when object exists")
	assert.eq(uerr:kind(), "Conflict", "precondition error should map to Conflict kind")

	-- Same precondition expressed via the only_if_absent alias.
	local _, uerr2 = storage:upload_object(key, "should not overwrite", {
		only_if_absent = true,
	})
	assert.not_nil(uerr2, "upload with only_if_absent=true should fail when object exists")
	assert.eq(uerr2:kind(), "Conflict", "only_if_absent should also map to Conflict kind")

	-- head_object on a missing key should error with NotFound kind.
	local missing, mherr = storage:head_object("head-test/does-not-exist.txt")
	assert.is_nil(missing, "head_object on missing key should not return a result")
	assert.not_nil(mherr, "head_object on missing key should return an error")
	assert.eq(mherr:kind(), "NotFound", "missing key error should map to NotFound kind")

	-- Headers escape-hatch: upload sending a raw x-amz-meta-* header,
	-- then verify it round-trips via head.metadata (the SDK channels
	-- x-amz-meta-* into the Metadata map). Also verify head.headers
	-- exposes the response headers we care about.
	local hkey = "head-test/with-headers.txt"
	local _, herr2 = storage:upload_object(hkey, "raw header upload", {
		headers = {
			["x-amz-meta-via-headers"] = "yes",
		},
	})
	assert.is_nil(herr2, "upload with raw headers should not error")

	local h2, herr3 = storage:head_object(hkey)
	assert.is_nil(herr3, "head_object should not error")
	assert.not_nil(h2, "head_object should return a result")
	assert.eq(h2.metadata["via-headers"], "yes",
		"x-amz-meta-* sent via raw headers should round-trip into metadata")
	assert.not_nil(h2.headers, "head.headers escape-hatch should be a table")
	assert.eq(type(h2.headers["content-length"]), "string",
		"response headers should include content-length (lowercased keys)")
	assert.eq(type(h2.headers["etag"]), "string",
		"response headers should include etag (lowercased keys)")

	storage:delete_objects({ hkey })

	-- Cleanup
	storage:delete_objects({ key })
	storage:release()

	return true
end

return { main = main }
