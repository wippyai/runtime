-- SPDX-License-Identifier: MPL-2.0

-- Test: Policy configuration methods
local assert = require("assert_primitives")

local function main()
	local html = require("html")

	-- Test allow_elements
	local policy1 = html.sanitize.new_policy()
	policy1:allow_elements("p", "div", "span")
	local result1 = policy1:sanitize('<p>text</p><script>bad</script>')
	assert.contains(result1, "<p>", "allowed p element preserved")
	assert.ok(not string.find(result1, "<script>"), "script stripped")

	-- Test allow_attrs with on_elements
	local policy2 = html.sanitize.new_policy()
	policy2:allow_elements("a")
	policy2:allow_attrs("href"):on_elements("a")
	local result2 = policy2:sanitize('<a href="https://example.com">link</a>')
	assert.contains(result2, "href", "href attribute allowed on a")

	-- Test allow_attrs with globally
	local policy3 = html.sanitize.new_policy()
	policy3:allow_elements("p", "div")
	policy3:allow_attrs("class"):globally()
	local result3 = policy3:sanitize('<p class="test">text</p>')
	assert.contains(result3, "class", "class attribute allowed globally")

	-- Test method chaining with UGC policy (already has href allowed)
	local policy4 = html.sanitize.ugc_policy()
	policy4:require_nofollow_on_links(true)
	local result4 = policy4:sanitize('<a href="https://example.com">link</a>')
	assert.contains(result4, "nofollow", "nofollow added to link")

	-- Test allow_images
	local policy5 = html.sanitize.new_policy()
	policy5:allow_images()
	local result5 = policy5:sanitize('<img src="test.jpg" alt="test">')
	assert.contains(result5, "<img", "img element allowed")
	assert.contains(result5, "alt", "alt attribute allowed")

	-- Test allow_lists
	local policy6 = html.sanitize.new_policy()
	policy6:allow_lists()
	local result6 = policy6:sanitize('<ul><li>item</li></ul>')
	assert.contains(result6, "<ul>", "ul element allowed")
	assert.contains(result6, "<li>", "li element allowed")

	-- Test allow_tables
	local policy7 = html.sanitize.new_policy()
	policy7:allow_tables()
	local result7 = policy7:sanitize('<table><tr><td>cell</td></tr></table>')
	assert.contains(result7, "<table>", "table element allowed")
	assert.contains(result7, "<td>", "td element allowed")

	return true
end

return { main = main }
