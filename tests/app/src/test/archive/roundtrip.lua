-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local archive = require("archive")
local fs = require("fs")

local function main()
	local vol, err = fs.get("app:temp")
	assert.is_nil(err, "temp fs error")
	assert.not_nil(vol, "temp fs handle")

	assert.ok(vol:writefile("/rt_src.bin", "streamed-from-fs"), "write source file")

	-- create a zip with add / add_dir / add_file
	local w, cerr = archive.create(vol, "/rt_out.zip")
	assert.is_nil(cerr, "create zip error")
	assert.ok(w:add("notes.txt", "hello world"), "add string entry")
	assert.ok(w:add_dir("docs"), "add directory entry")
	assert.ok(w:add_file("docs/src.bin", vol, "/rt_src.bin"), "add file streamed from fs")
	assert.ok(w:close(), "close writer")

	-- open random reader and exercise entries / stat / read
	local r, oerr = archive.open(vol, "/rt_out.zip")
	assert.is_nil(oerr, "open zip error")

	local names = {}
	for e in r:entries() do
		names[e.name] = e
	end
	assert.not_nil(names["notes.txt"], "entries include notes.txt")
	assert.not_nil(names["docs/src.bin"], "entries include docs/src.bin")

	local info, serr = r:stat("notes.txt")
	assert.is_nil(serr, "stat error")
	assert.eq(info.size, 11, "notes.txt size")
	assert.eq(info.type, "file", "notes.txt type")

	local data, rerr = r:read("notes.txt")
	assert.is_nil(rerr, "read error")
	assert.eq(data, "hello world", "read content")

	-- extract everything to a subdir and verify on the fs
	local n, xerr = r:extract_all(vol, { prefix = "rt_extracted/" })
	assert.is_nil(xerr, "extract_all error")
	assert.ok(n >= 2, "extract_all count")
	assert.ok(r:close(), "close reader")

	assert.eq(vol:readfile("rt_extracted/notes.txt"), "hello world", "extracted notes.txt")
	assert.eq(vol:readfile("rt_extracted/docs/src.bin"), "streamed-from-fs", "extracted src.bin")

	-- tar round-trip through the same API
	local tw, terr = archive.create(vol, "/rt_out.tar", { format = "tar" })
	assert.is_nil(terr, "create tar error")
	assert.ok(tw:add("x.txt", "tar-content"), "tar add")
	assert.ok(tw:close(), "tar close")

	local tr, toerr = archive.open(vol, "/rt_out.tar")
	assert.is_nil(toerr, "open tar error")
	assert.eq(tr:read("x.txt"), "tar-content", "tar read")
	assert.ok(tr:close(), "tar reader close")

	vol:remove("/rt_src.bin")
	vol:remove("/rt_out.zip")
	vol:remove("/rt_out.tar")
	return true
end

return { main = main }
