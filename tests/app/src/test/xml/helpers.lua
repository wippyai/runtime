-- SPDX-License-Identifier: MPL-2.0

-- Test: xml element helpers and paths
local assert = require("assert2")
local xml = require("xml")

local function main(): boolean
	local doc, err = xml.decode([[<s:catalog xmlns:s="urn:shop" xmlns:xsi="urn:xsi">
<s:item id="1" xsi:type="s:Book"><title>Go</title><price>10</price></s:item>
<s:item id="2"><title>Lua</title></s:item>
<group><s:item id="3"><title>XML</title></s:item></group>
</s:catalog>]], {whitespace = "strip"})
	assert.is_nil(err, "decode error")
	if not doc then
		return false
	end

	assert.eq(xml.text(doc), "Go10LuaXML", "concatenated text")
	local within, budget_err = pcall(xml.text, doc, {max_node_bytes = 8})
	assert.ok(not within, "a walk past its budget raises")
	assert.eq(budget_err:details().limit, "max_node_bytes", "the budget error names the option")
	assert.eq(#xml.children(doc), 3, "child elements")
	assert.eq(#xml.children(doc, "item", "urn:shop"), 2, "filtered children")
	assert.is_nil(xml.child(doc, "item", ""), "items have a namespace")

	local first = xml.child(doc, "item")
	assert.not_nil(first, "first item")
	if not first then
		return false
	end
	assert.eq(xml.attr(first, "id"), "1", "attribute")
	assert.eq(xml.attr(first, "type", "urn:xsi"), "s:Book", "qualified attribute")

	local local_name, ns, rerr = xml.resolve(doc, xml.attr(first, "type", "urn:xsi") or "")
	assert.is_nil(rerr, "resolve error")
	assert.eq(local_name, "Book", "resolved local name")
	assert.eq(ns, "urn:shop", "resolved namespace")

	local ids = {}
	local nested, ferr = xml.find(doc, "*/p:item", {ns = {p = "urn:shop"}})
	assert.is_nil(ferr, "find error")
	for _, item in ipairs(nested or {}) do
		ids[#ids + 1] = xml.attr(item, "id") or "?"
	end
	assert.eq(table.concat(ids, ","), "3", "path relative to the element")

	local titles = {}
	for _, title in ipairs(xml.find(doc, "p:item/title", {ns = {p = "urn:shop"}}) or {}) do
		titles[#titles + 1] = xml.text(title)
	end
	assert.eq(table.concat(titles, ","), "Go,Lua", "two-step path")
	local any = xml.find(doc, "*:item") or {}
	assert.eq(#any, 2, "any namespace")

	local missing, perr = xml.find(doc, "q:item")
	assert.is_nil(missing, "an unbound prefix finds nothing")
	assert.eq(perr and perr:details().code, "namespace", "find returns the path error")
	return true
end

return { main = main }
