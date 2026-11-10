-- SPDX-License-Identifier: MPL-2.0

-- Test: xml record selection over files and archive streams
local assert = require("assert2")
local xml = require("xml")
local fs = require("fs")
local archive = require("archive")

local function feed(count: integer): string
	local parts = {'<s:catalog xmlns:s="urn:shop" xmlns:xsi="urn:xsi">'}
	for i = 1, count do
		parts[#parts + 1] = '<s:item id="' .. i .. '" xsi:type="s:Book" status="' ..
			(i % 3 == 0 and "retired" or "active") .. '"><s:title>Item ' .. i .. '</s:title></s:item>'
	end
	parts[#parts + 1] = "<s:summary count=\"" .. count .. "\"/></s:catalog>"
	return table.concat(parts)
end

local function consume(source: any): (integer, integer, string)
	local reader, err = xml.reader(source)
	assert.is_nil(err, "reader error")
	local records, serr = reader:select({"s:catalog/s:item", "s:catalog/s:summary"}, {ns = {s = "urn:shop"}})
	assert.is_nil(serr, "select error")
	local active, total, summary = 0, 0, ""
	while true do
		local batch, rerr = records:read(100)
		assert.is_nil(rerr, "read error")
		if not batch then
			break
		end
		assert.ok(#batch <= 100, "batch size")
		for _, record in ipairs(batch) do
			if record.name == "summary" then
				summary = xml.attr(record, "count") or ""
			else
				total = total + 1
				if xml.attr(record, "status") == "active" then
					active = active + 1
				end
				local local_name, ns = xml.resolve(record, xml.attr(record, "type", "urn:xsi") or "")
				assert.eq(local_name, "Book", "detached QName")
				assert.eq(ns, "urn:shop", "detached namespace")
			end
		end
	end
	assert.ok(records:close(), "close records")
	return total, active, summary
end

local function main(): boolean
	local document = feed(3000)
	local total, active, summary = consume(document)
	assert.eq(total, 3000, "string records")
	assert.eq(active, 2000, "active records")
	assert.eq(summary, "3000", "summary record")

	local vol, err = fs.get("app:temp")
	assert.is_nil(err, "temp fs")
	local base = "/xml_records_" .. os.time()
	assert.ok(vol:writefile(base .. ".xml", document), "write feed")
	local file = vol:open(base .. ".xml", "r")
	total, active = consume(file)
	assert.eq(total, 3000, "file records")
	assert.eq(active, 2000, "file active records")
	file:close()

	local zip, zerr = archive.create(vol, base .. ".zip")
	assert.is_nil(zerr, "create archive")
	assert.ok(zip:add("feed.xml", document), "add feed")
	assert.ok(zip:close(), "close archive")
	local packed = archive.open(vol, base .. ".zip")
	local stream, serr = packed:stream("feed.xml")
	assert.is_nil(serr, "entry stream")
	total, active = consume(stream)
	assert.eq(total, 3000, "stream records")
	assert.eq(active, 2000, "stream active records")
	stream:close()
	packed:close()

	vol:remove(base .. ".xml")
	vol:remove(base .. ".zip")
	return true
end

return { main = main }
