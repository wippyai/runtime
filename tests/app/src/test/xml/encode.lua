-- SPDX-License-Identifier: MPL-2.0

-- Test: xml.encode serialisation and options
local assert = require("assert2")
local xml = require("xml")

local function main(): boolean
	local order: xml.Element = {
		name = "order",
		ns = "urn:shop",
		attrs = {id = "7", ["{urn:ext}source"] = "api"},
		children = {
			{name = "total", ns = "urn:shop", attrs = {currency = "USD"}, text = "12.50"},
			{name = "note", ns = "urn:shop", children = {"a < b & ", {name = "b", ns = "urn:shop", text = "c"}}},
		},
	}

	local out, err = xml.encode(order)
	assert.is_nil(err, "encode error")
	assert.eq(out, '<order xmlns="urn:shop" xmlns:ns1="urn:ext" id="7" ns1:source="api">' ..
		'<total currency="USD">12.50</total><note>a &lt; b &amp; <b>c</b></note></order>', "deterministic output")

	local pretty = xml.encode(order, {indent = "  ", declaration = true, ns = {s = "urn:shop", e = "urn:ext"}})
	assert.eq(pretty, '<?xml version="1.0" encoding="UTF-8"?>\n' ..
		'<s:order xmlns:e="urn:ext" xmlns:s="urn:shop" id="7" e:source="api">\n' ..
		'  <s:total currency="USD">12.50</s:total>\n' ..
		'  <s:note>a &lt; b &amp; <s:b>c</s:b></s:note>\n' ..
		'</s:order>', "preferred prefixes and indentation")

	local back, derr = xml.decode(out or "")
	assert.is_nil(derr, "output is well formed")
	assert.eq(xml.encode(back or order), out, "round trip is stable")

	local unqualified = xml.encode({name = "a", ns = "urn:d", children = {{name = "b"}}})
	assert.eq(unqualified, '<a xmlns="urn:d"><b xmlns=""/></a>', "elements without namespace undeclare the default")
	return true
end

return { main = main }
