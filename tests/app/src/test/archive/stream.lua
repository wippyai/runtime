-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local archive = require("archive")
local fs = require("fs")

local function main()
	local vol, err = fs.get("app:temp")
	assert.is_nil(err, "temp fs error")

	local payload = string.rep("abcdefghij", 100) -- 1000 bytes

	local w, cerr = archive.create(vol, "/st_out.zip")
	assert.is_nil(cerr, "create error")
	assert.ok(w:add("big.txt", payload), "add entry")
	assert.ok(w:add("small.txt", "tiny"), "add small")
	assert.ok(w:close(), "close writer")

	local r, oerr = archive.open(vol, "/st_out.zip")
	assert.is_nil(oerr, "open error")

	-- read the entry as a real stream.Stream, pulling chunks through the
	-- stream subsystem (this exercises the dispatcher-backed read path)
	local s, serr = r:stream("big.txt")
	assert.is_nil(serr, "stream error")
	assert.not_nil(s, "stream handle")

	local parts = {}
	while true do
		local chunk, rerr = s:read(64)
		assert.is_nil(rerr, "stream read error")
		if chunk == nil then
			break
		end
		parts[#parts + 1] = chunk
	end
	s:close()

	local got = table.concat(parts)
	assert.eq(#got, 1000, "streamed length")
	assert.eq(got, payload, "streamed content matches")

	-- the entry stream composes with fs:writefile too
	local s2, s2err = r:stream("small.txt")
	assert.is_nil(s2err, "second stream error")
	assert.ok(vol:writefile("/st_small_copy.txt", s2), "writefile from entry stream")
	assert.eq(vol:readfile("/st_small_copy.txt"), "tiny", "piped content")
	assert.ok(r:close(), "close reader")

	vol:remove("/st_out.zip")
	vol:remove("/st_small_copy.txt")
	return true
end

return { main = main }
