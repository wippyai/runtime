-- Test: Tree-sitter Markdown language support
local assert = require("assert_primitives")

local function main()
	local treesitter = require("treesitter")

	-- Verify Markdown is in supported languages
	local langs = treesitter.supported_languages()
	assert.ok(langs["markdown"], "Markdown is supported")

	-- Test parsing Markdown code
	local code = [[
# Main Title

This is a paragraph with **bold** and *italic* text.

## Section One

Here's a list:

- Item 1
- Item 2
- Item 3

### Subsection

1. Numbered item
2. Another item

## Section Two

> This is a blockquote
> spanning multiple lines

```go
package main

func main() {
    println("Hello")
}
```

Here's a [link](https://example.com) and an image:

![Alt text](image.png)

| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |

---

The end.
]]

	local tree = treesitter.parse("markdown", code)
	assert.not_nil(tree, "parse returns tree")

	local root = tree:root_node()
	assert.eq(root:kind(), "document", "root is document")
	assert.ok(not root:has_error(), "no parse errors")

	-- Query for headings
	local heading_query = treesitter.query("markdown", [[
        (atx_heading) @heading
    ]])
	local heading_captures = heading_query:captures(root, code)
	assert.eq(#heading_captures, 4, "found 4 headings")
	heading_query:close()

	-- Query for paragraphs
	local para_query = treesitter.query("markdown", [[
        (paragraph) @para
    ]])
	local para_captures = para_query:captures(root, code)
	assert.ok(#para_captures >= 3, "found paragraphs")
	para_query:close()

	-- Query for lists
	local list_query = treesitter.query("markdown", [[
        (list) @list
    ]])
	local list_captures = list_query:captures(root, code)
	assert.eq(#list_captures, 2, "found 2 lists")
	list_query:close()

	-- Query for code blocks
	local code_query = treesitter.query("markdown", [[
        (fenced_code_block) @code_block
    ]])
	local code_captures = code_query:captures(root, code)
	assert.eq(#code_captures, 1, "found 1 code block")
	code_query:close()

	-- Query for blockquotes
	local quote_query = treesitter.query("markdown", [[
        (block_quote) @quote
    ]])
	local quote_captures = quote_query:captures(root, code)
	assert.eq(#quote_captures, 1, "found 1 blockquote")
	quote_query:close()

	-- Query for links (may vary by tree-sitter-markdown version)
	local link_query = treesitter.query("markdown", [[
        (inline_link) @link
    ]])
	if link_query then
		local link_captures = link_query:captures(root, code)
		assert.ok(#link_captures >= 1, "found links")
		link_query:close()
	end

	-- Query for images (may vary by tree-sitter-markdown version)
	local img_query = treesitter.query("markdown", [[
        (image) @image
    ]])
	if img_query then
		local img_captures = img_query:captures(root, code)
		assert.ok(#img_captures >= 1, "found image")
		img_query:close()
	end

	-- Query for thematic breaks (may vary by tree-sitter-markdown version)
	local break_query = treesitter.query("markdown", [[
        (thematic_break) @break
    ]])
	if break_query then
		local break_captures = break_query:captures(root, code)
		assert.ok(#break_captures >= 1, "found thematic break")
		break_query:close()
	end

	-- Test cursor navigation
	local cursor = tree:walk()
	cursor:goto_first_child()

	-- Count top-level elements
	local top_level_count = 1
	while cursor:goto_next_sibling() do
		top_level_count = top_level_count + 1
	end
	assert.ok(top_level_count >= 1, "has top-level elements")

	cursor:close()

	-- Test with md alias (needs proper markdown structure)
	local tree2 = treesitter.parse("md", "# Hello\n\nThis is a paragraph.")
	assert.not_nil(tree2, "md alias works")
	assert.ok(not tree2:root_node():has_error(), "md alias parses without error")
	tree2:close()

	-- Test language object
	local lang = treesitter.language("markdown")
	assert.not_nil(lang, "language object created")
	assert.ok(lang:version() > 0, "has version")

	tree:close()

	return true
end

return { main = main }
