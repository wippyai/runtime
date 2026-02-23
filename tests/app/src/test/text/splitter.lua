-- SPDX-License-Identifier: MPL-2.0

-- Test: text.splitter functionality
local assert = require("assert2")

local function main()
	local text = require("text")

	-- Test recursive splitter creation
	local splitter, err = text.splitter.recursive({chunk_size = 100, chunk_overlap = 20})
	assert.is_nil(err, "recursive splitter creation should not error")
	assert.not_nil(splitter, "splitter object returned")

	-- Test recursive splitter with defaults
	local splitter2, err2 = text.splitter.recursive()
	assert.is_nil(err2, "recursive splitter with defaults should not error")
	assert.not_nil(splitter2, "splitter object returned")

	-- Test markdown splitter creation
	local md_splitter, err3 = text.splitter.markdown({chunk_size = 200})
	assert.is_nil(err3, "markdown splitter creation should not error")
	assert.not_nil(md_splitter, "markdown splitter object returned")

	-- Test markdown splitter with all options
	local md_splitter2, err4 = text.splitter.markdown({
		chunk_size = 500,
		chunk_overlap = 50,
		code_blocks = true,
		heading_hierarchy = true
	})
	assert.is_nil(err4, "markdown splitter with options should not error")
	assert.not_nil(md_splitter2, "markdown splitter object returned")

	-- Test split_text
	local longText = "This is a long text that needs to be split into multiple chunks. " ..
	"Each chunk should respect the configured chunk size with proper overlap. " ..
	"The splitter uses recursive character splitting by default."
	local chunks, split_err = splitter:split_text(longText)
	assert.is_nil(split_err, "split_text should not error")
	assert.not_nil(chunks, "chunks returned")
	assert.ok(#chunks >= 1, "at least one chunk returned")

	-- Test split_batch
	local pages = {
		{content = "First page content that might be split", metadata = {page = 1, source = "doc1"}},
		{content = "Second page with different content", metadata = {page = 2, source = "doc1"}}
	}
	local batch_chunks, batch_err = splitter:split_batch(pages)
	assert.is_nil(batch_err, "split_batch should not error")
	assert.not_nil(batch_chunks, "batch chunks returned")
	assert.ok(#batch_chunks >= 1, "at least one chunk from batch")

	-- Verify metadata is preserved
	assert.not_nil(batch_chunks[1].metadata, "metadata preserved in chunk")
	assert.not_nil(batch_chunks[1].content, "content present in chunk")

	-- Test markdown splitter with markdown content
	local md_content = [[
# Heading 1

This is a paragraph under heading 1.

## Heading 2

Another paragraph here with more content.

- List item 1
- List item 2
- List item 3

```lua
local x = 1
```
]]
	local md_chunks, md_split_err = md_splitter:split_text(md_content)
	assert.is_nil(md_split_err, "markdown split_text should not error")
	assert.not_nil(md_chunks, "markdown chunks returned")

	return true
end

return { main = main }
