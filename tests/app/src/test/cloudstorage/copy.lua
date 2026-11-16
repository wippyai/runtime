-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage copy_object (server-side copy)
local assert = require("assert_primitives")

local function sleep_seconds(n)
	local time = require("time")
	time.sleep(n .. "s")
end

local function main()
	local cloudstorage = require("cloudstorage")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	local src = "copy-test/source.txt"
	local dst = "copy-test/destination.txt"
	storage:delete_objects({ src, dst })

	local _, uerr = storage:upload_object(src, "copy me", {
		content_type = "text/plain",
		metadata = { owner = "tests" },
	})
	assert.is_nil(uerr, "upload should not error")

	-- Default COPY directive keeps content type and user metadata.
	local copied, cerr = storage:copy_object(src, dst)
	assert.is_nil(cerr, "copy_object should not error")
	assert.not_nil(copied, "copy_object should return a result")
	assert.eq(type(copied.etag), "string", "result etag should be a string")
	assert.eq(copied.etag ~= "", true, "result etag should not be empty")

	local head, herr = storage:head_object(dst)
	assert.is_nil(herr, "head of copy should not error")
	assert.eq(head.size, #"copy me", "copy should have the source size")
	assert.eq(head.content_type, "text/plain", "COPY keeps content type")
	assert.eq(head.metadata.owner, "tests", "COPY keeps user metadata")

	-- if_match against a stale source etag is a precondition failure.
	local none, perr = storage:copy_object(src, dst, { if_match = '"deadbeef"' })
	assert.is_nil(none, "stale if_match should not return a result")
	assert.not_nil(perr, "stale if_match should error")
	assert.eq(perr:kind(), "Conflict", "precondition error should map to Conflict kind")

	-- if_none_match = "*" refuses an existing destination.
	local _, nerr = storage:copy_object(src, dst, { if_none_match = "*" })
	assert.not_nil(nerr, "if_none_match=* should fail when destination exists")
	assert.eq(nerr:kind(), "Conflict", "destination precondition should map to Conflict kind")

	-- A missing source is NotFound.
	local _, merr = storage:copy_object("copy-test/absent.txt", "copy-test/never.txt")
	assert.not_nil(merr, "missing source should error")
	assert.eq(merr:kind(), "NotFound", "missing source should map to NotFound kind")

	-- Bad directive is rejected before reaching the provider.
	local _, derr = storage:copy_object(src, dst, { metadata_directive = "MERGE" })
	assert.not_nil(derr, "unknown directive should error")
	assert.eq(derr:kind(), "Invalid", "unknown directive should map to Invalid kind")

	-- Copy onto itself with REPLACE refreshes last_modified.
	local before = storage:head_object(src)
	sleep_seconds(2)
	local touched, terr = storage:copy_object(src, src, {
		metadata_directive = "REPLACE",
		content_type = "text/plain",
		metadata = { owner = "tests", touched = "yes" },
	})
	assert.is_nil(terr, "self copy with REPLACE should not error")
	assert.not_nil(touched, "self copy should return a result")
	local after = storage:head_object(src)
	assert.eq(after.last_modified > before.last_modified, true, "last_modified should advance")
	assert.eq(after.metadata.touched, "yes", "REPLACE metadata should be stored")

	-- Self copy without REPLACE is invalid.
	local _, serr = storage:copy_object(src, src)
	assert.not_nil(serr, "self copy with COPY should error")
	assert.eq(serr:kind(), "Invalid", "self copy with COPY should map to Invalid kind")

	storage:delete_objects({ src, dst })
	storage:release()

	return true
end

return { main = main }
