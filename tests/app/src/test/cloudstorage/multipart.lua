-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage presigned multipart upload round trip
local assert = require("assert_primitives")

-- ETag header casing differs between providers; scan case-insensitively.
local function etag_of(resp)
	for k, v in pairs(resp.headers or {}) do
		if string.lower(k) == "etag" then
			return (tostring(v):gsub('"', ""))
		end
	end
	return nil
end

local function main()
	local cloudstorage = require("cloudstorage")
	local http_client = require("http_client")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	local key = "multipart-test/big.bin"

	-- Parts before the last must be at least 5 MiB (S3 protocol minimum).
	local part1 = string.rep("a", 5 * 1024 * 1024)
	local part2 = string.rep("b", 1024)

	-- Start the multipart upload
	local created, cerr = storage:create_multipart_upload(key, {
		content_type = "application/octet-stream",
		metadata = { source = "multipart-e2e" },
	})
	assert.is_nil(cerr, "create_multipart_upload should not error")
	assert.not_nil(created, "should have create result")
	assert.eq(type(created.upload_id), "string", "upload_id should be string")
	assert.eq(#created.upload_id > 0, true, "upload_id should be non-empty")

	-- Presign both parts in one batch
	local urls, perr = storage:presigned_part_urls(key, created.upload_id, {
		parts = { 1, 2 },
		expiration = 900,
	})
	assert.is_nil(perr, "presigned_part_urls should not error")
	assert.eq(#urls, 2, "should have 2 part URLs")
	assert.eq(urls[1].part_number, 1, "first URL part_number")
	assert.eq(urls[2].part_number, 2, "second URL part_number")
	assert.eq(urls[1].url:match("^https?://") ~= nil, true, "part URL should be http(s)")

	-- Upload both parts through the presigned URLs (as a browser would)
	local r1, e1 = http_client.put(urls[1].url, { body = part1 })
	assert.is_nil(e1, "part 1 PUT should not error")
	assert.eq(r1.status_code, 200, "part 1 PUT status")
	local etag1 = etag_of(r1)
	assert.not_nil(etag1, "part 1 should return an ETag")

	local r2, e2 = http_client.put(urls[2].url, { body = part2 })
	assert.is_nil(e2, "part 2 PUT should not error")
	assert.eq(r2.status_code, 200, "part 2 PUT status")
	local etag2 = etag_of(r2)
	assert.not_nil(etag2, "part 2 should return an ETag")

	-- Complete with parts deliberately out of order — the runtime sorts them
	local done, derr = storage:complete_multipart_upload(key, created.upload_id, {
		{ part_number = 2, etag = etag2 },
		{ part_number = 1, etag = etag1 },
	})
	assert.is_nil(derr, "complete_multipart_upload should not error")
	assert.not_nil(done, "should have completion result")
	assert.eq(type(done.etag), "string", "final etag should be string")

	-- The assembled object must have the combined size and content
	local head, herr = storage:head_object(key)
	assert.is_nil(herr, "head_object should not error")
	assert.eq(head.size, #part1 + #part2, "assembled size should match")
	assert.eq(head.metadata.source, "multipart-e2e", "metadata should survive")

	-- Spot-check the stitched content across the part boundary
	local url, uerr = storage:presigned_get_url(key)
	assert.is_nil(uerr, "presigned_get_url should not error")
	local got, gerr = http_client.get(url, {
		headers = { Range = "bytes=" .. (#part1 - 4) .. "-" .. (#part1 + 3) },
	})
	assert.is_nil(gerr, "ranged GET should not error")
	assert.eq(got.body, "aaaabbbb", "content across part boundary")

	-- Abort flow: a second upload discarded before completion
	local created2, cerr2 = storage:create_multipart_upload("multipart-test/aborted.bin")
	assert.is_nil(cerr2, "second create should not error")
	local ok, aerr = storage:abort_multipart_upload("multipart-test/aborted.bin", created2.upload_id)
	assert.is_nil(aerr, "abort should not error")
	assert.eq(ok, true, "abort should return true")

	-- Completing an aborted upload must fail
	local _, failerr = storage:complete_multipart_upload("multipart-test/aborted.bin", created2.upload_id, {
		{ part_number = 1, etag = "deadbeef" },
	})
	assert.not_nil(failerr, "completing an aborted upload should fail")

	-- Validation errors surface without network calls
	local _, verr = storage:presigned_part_urls(key, created.upload_id, {})
	assert.not_nil(verr, "missing parts/count should error")

	-- Cleanup
	storage:delete_objects({ key })
	storage:release()

	return true
end

return { main = main }
