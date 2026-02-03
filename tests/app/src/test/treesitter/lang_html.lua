-- Test: Tree-sitter HTML language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify HTML is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["html"], "HTML is supported")

	-- Test parsing HTML code
	local code = [[
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Test Page</title>
    <link rel="stylesheet" href="styles.css">
    <script src="app.js" defer></script>
</head>
<body>
    <header id="main-header" class="header">
        <nav>
            <ul>
                <li><a href="/">Home</a></li>
                <li><a href="/about">About</a></li>
            </ul>
        </nav>
    </header>

    <main class="content">
        <article>
            <h1>Welcome</h1>
            <p>This is a <strong>test</strong> page.</p>
            <img src="image.png" alt="An image">
        </article>
    </main>

    <footer>
        <p>&copy; 2024</p>
    </footer>
</body>
</html>
]]

	local tree = treesitter.parse("html", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "document", "root is document")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for all elements
	local elem_query = treesitter.query("html", [[
        (element (start_tag (tag_name) @tag))
    ]])
	local elem_captures = elem_query:captures(root, code)
	assert.ok(#elem_captures >= 15, "found many elements")
	elem_query:close()

	-- Query for specific tags
	local header_query = treesitter.query("html", [[
        (element
            (start_tag (tag_name) @tag)
            (#eq? @tag "header"))
    ]])
	local header_captures = header_query:captures(root, code)
	assert.eq(#header_captures, 1, "found 1 header")
	header_query:close()

	-- Query for elements with id attribute
	local id_query = treesitter.query("html", [[
        (element
            (start_tag
                (attribute
                    (attribute_name) @attr_name
                    (quoted_attribute_value) @attr_value)
                (#eq? @attr_name "id")))
    ]])
	local id_captures = id_query:captures(root, code)
	assert.ok(#id_captures >= 2, "found elements with id")
	id_query:close()

	-- Query for links
	local link_query = treesitter.query("html", [[
        (element
            (start_tag
                (tag_name) @tag
                (attribute
                    (attribute_name) @attr
                    (quoted_attribute_value) @href))
            (#eq? @tag "a")
            (#eq? @attr "href"))
    ]])
	local link_captures = link_query:captures(root, code)
	assert.ok(#link_captures >= 4, "found links with href")
	link_query:close()

	-- Query for void elements (like meta, link, img)
	local void_query = treesitter.query("html", [[
        (element (start_tag (tag_name) @tag))
    ]])
	local void_captures = void_query:captures(root, code)
	assert.ok(#void_captures >= 5, "found elements including void tags")
	void_query:close()

	-- Test node navigation
	local cursor = tree:walk()
	cursor:goto_first_child()

	-- Find the html element
	local found_html = false
	repeat
		local node = cursor:current_node()
		if node:kind() == "element" then
			local first_child = node:child(0)
			if first_child and first_child:kind() == "start_tag" then
				found_html = true
				break
			end
		end
	until not cursor:goto_next_sibling()
	assert.ok(found_html, "found html element")

	cursor:close()

	-- Test with html5 alias
	local tree2 = treesitter.parse("html5", "<div>test</div>")
	assert.not_nil(tree2, "html5 alias works")
	tree2:close()

	-- Test language object
	local lang = treesitter.language("html")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	tree:close()

	return true
end

return { main = main }
