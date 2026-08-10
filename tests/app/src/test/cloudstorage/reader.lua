-- SPDX-License-Identifier: MPL-2.0

-- Test: open_reader ranged random access + archive.open over an S3 object
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")
	local archive = require("archive")
	local fs = require("fs")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")

	local vol, ferr = fs.get("app:temp")
	assert.is_nil(ferr, "temp fs error")

	-- Build a zip locally: a small text entry plus one spanning many blocks
	local big = string.rep("0123456789abcdef", 20 * 1024) -- 320 KiB
	local w, cerr = archive.create(vol, "/reader-e2e.zip")
	assert.is_nil(cerr, "create zip error")
	assert.ok(w:add("hello.txt", "hello from s3"), "add text entry")
	assert.ok(w:add("data/big.bin", big), "add big entry")
	assert.ok(w:close(), "close writer")

	local zip_bytes, rerr = vol:readfile("/reader-e2e.zip")
	assert.is_nil(rerr, "read zip bytes error")

	-- Park the archive in object storage
	local key = "reader-test/archive.zip"
	local ok, uerr = storage:upload_object(key, zip_bytes, { content_type = "application/zip" })
	assert.is_nil(uerr, "upload zip should not error")
	assert.eq(ok, true, "upload should return true")

	-- Open a ranged reader over the object; small blocks force several
	-- ranged GETs so the cache path is actually exercised.
	local reader, oerr = storage:open_reader(key, { block_size = 65536, cache_blocks = 2 })
	assert.is_nil(oerr, "open_reader should not error")
	assert.not_nil(reader, "should have reader")
	assert.eq(reader:size(), #zip_bytes, "reader size should match object size")
	assert.eq(reader:key(), key, "reader key should match")

	-- Random access straight from object storage — no local staging
	local r, aerr = archive.open(reader)
	assert.is_nil(aerr, "archive.open over reader should not error")

	local names = {}
	for e in r:entries() do
		names[e.name] = e
	end
	assert.not_nil(names["hello.txt"], "entries include hello.txt")
	assert.not_nil(names["data/big.bin"], "entries include data/big.bin")
	assert.eq(names["data/big.bin"].size, #big, "big entry size")

	assert.eq(r:read("hello.txt"), "hello from s3", "read small entry")

	-- Stream the large entry and verify content end to end
	local es, serr = r:stream("data/big.bin")
	assert.is_nil(serr, "stream entry error")
	local chunks = {}
	while true do
		local chunk = es:read(65536)
		if not chunk then
			break
		end
		chunks[#chunks + 1] = chunk
	end
	assert.eq(table.concat(chunks), big, "streamed big entry content")

	-- Fan-out path used by extraction workers: an archive entry stream is
	-- uploaded straight back to object storage (ReaderProvider source),
	-- without materializing the entry in memory.
	local es2, serr2 = r:stream("data/big.bin")
	assert.is_nil(serr2, "second stream open error")
	local child_key = "reader-test/child/big.bin"
	local ok2, uerr2 = storage:upload_object(child_key, es2, { content_type = "application/octet-stream" })
	assert.is_nil(uerr2, "upload from entry stream should not error")
	assert.eq(ok2, true, "stream upload should return true")

	local child_head, cherr = storage:head_object(child_key)
	assert.is_nil(cherr, "child head_object error")
	assert.eq(child_head.size, #big, "child object size matches entry")

	assert.ok(r:close(), "close archive reader")
	assert.ok(reader:close(), "close ranged reader")

	-- Missing objects surface as errors at open time
	local missing, merr = storage:open_reader("reader-test/missing.zip")
	assert.is_nil(missing, "missing object should not return a reader")
	assert.not_nil(merr, "missing object should error")

	-- Cleanup
	storage:delete_objects({ key, "reader-test/child/big.bin" })
	storage:release()

	return true
end

return { main = main }
