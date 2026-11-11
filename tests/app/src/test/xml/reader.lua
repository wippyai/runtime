-- SPDX-License-Identifier: MPL-2.0

-- Test: xml reader cursor over strings and files
local assert = require("assert2")
local xml = require("xml")
local fs = require("fs")

local document = [[<?xml version="1.0"?>
<!--feed--><feed xmlns="urn:feed">
  <entry id="1"><title>One</title><body>first</body></entry>
  <entry id="2"><title>Two</title><body>second</body></entry>
  <entry id="3" draft="true"><title>Three</title></entry>
</feed>]]

local function walk(reader: xml.Reader): string
	local seen = {}
	while true do
		local kind, err = reader:next()
		assert.is_nil(err, "next error")
		if not kind then
			break
		end
		if kind == "start" and reader:name() == "entry" then
			local pos = reader:pos()
			assert.eq(reader:depth(), 2, "entry depth")
			assert.eq(reader:ns(), "urn:feed", "entry namespace")
			assert.ok(pos.line >= 3, "entry line")
			if reader:attr("draft") == "true" then
				local skipped, serr = reader:skip()
				assert.is_nil(serr, "skip error")
				assert.ok(skipped, "skip result")
			else
				local entry, nerr = reader:node()
				assert.is_nil(nerr, "node error")
				if entry then
					local title = xml.child(entry, "title")
					seen[#seen + 1] = (xml.attr(entry, "id") or "?") .. ":" .. (title and xml.text(title) or "")
					assert.eq(entry.xmlns and entry.xmlns[""], "urn:feed", "a detached entry carries its scope")
				end
			end
			assert.eq(reader:kind(), "end", "the cursor rests on the end event")
		end
	end
	assert.is_nil(reader:kind(), "no current event after the document")
	return table.concat(seen, ",")
end

local function main(): boolean
	local reader, err = xml.reader(document, {whitespace = "strip"})
	assert.is_nil(err, "reader error")
	if not reader then
		return false
	end
	assert.eq(walk(reader), "1:One,2:Two", "string source")
	assert.ok(reader:close(), "close")
	assert.ok(reader:close(), "close is idempotent")

	local events = xml.reader(document, {comments = true})
	if events then
		local kind = events:next()
		assert.eq(kind, "comment", "comments are reported when enabled")
		assert.eq(events:text(), "feed", "comment text")
		events:close()
	end

	local vol, verr = fs.get("app:temp")
	assert.is_nil(verr, "temp fs")
	local path = "/xml_reader_" .. os.time() .. ".xml"
	assert.ok(vol:writefile(path, document), "write fixture")
	local file, ferr = vol:open(path, "r")
	assert.is_nil(ferr, "open fixture")
	local from_file = xml.reader(file, {whitespace = "strip"})
	assert.not_nil(from_file, "file reader")
	if from_file then
		assert.eq(walk(from_file), "1:One,2:Two", "file source")
		from_file:close()
	end
	file:close()
	vol:remove(path)
	return true
end

return { main = main }
