package text

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestTextModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewTextModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local text = require("text")
			assert(type(text) == "table", "module should be a table")
			assert(type(text.splitter) == "table", "splitter should be a table")
			assert(type(text.splitter.recursive) == "function", "recursive should be a function")
			assert(type(text.splitter.markdown) == "function", "markdown should be a function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("recursive splitter creation", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "with default options",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({})
					assert(splitter ~= nil, "splitter should not be nil")
					assert(err == nil, "error should be nil")
					assert(type(splitter.split_text) == "function", "should have split_text method")
					assert(type(splitter.split_batch) == "function", "should have split_batch method")
				`,
			},
			{
				name: "with custom options",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({
						chunk_size = 500,
						chunk_overlap = 50,
						separators = {"\n\n", "\n", " "},
						keep_separator = true
					})
					assert(splitter ~= nil, "splitter should not be nil")
					assert(err == nil, "error should be nil")
				`,
			},
			{
				name: "no options provided",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive()
					assert(splitter ~= nil, "splitter should not be nil")
					assert(err == nil, "error should be nil")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTextModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("markdown splitter creation", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "with default options",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.markdown({})
					assert(splitter ~= nil, "splitter should not be nil")
					assert(err == nil, "error should be nil")
					assert(type(splitter.split_text) == "function", "should have split_text method")
					assert(type(splitter.split_batch) == "function", "should have split_batch method")
				`,
			},
			{
				name: "with custom options",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.markdown({
						chunk_size = 800,
						chunk_overlap = 100,
						code_blocks = true,
						reference_links = false,
						heading_hierarchy = true,
						join_table_rows = false
					})
					assert(splitter ~= nil, "splitter should not be nil")
					assert(err == nil, "error should be nil")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTextModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("text splitting functionality", func(t *testing.T) {
		testCases := []struct {
			name           string
			script         string
			expectedChunks int
			minChunks      int
		}{
			{
				name: "simple text splitting",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({
						chunk_size = 50,
						chunk_overlap = 10
					})
					assert(err == nil, "error creating splitter should be nil")
					
					local test_text = "This is a test document. It has multiple sentences. Each sentence should be handled properly by the text splitter."
					local chunks, err = splitter:split_text(test_text)
					assert(err == nil, "error splitting text should be nil")
					assert(type(chunks) == "table", "chunks should be a table")
					assert(#chunks > 0, "should have at least one chunk")
					
					return #chunks
				`,
				minChunks: 2,
			},
			{
				name: "paragraph splitting",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({
						chunk_size = 100,
						chunk_overlap = 20,
						separators = {"\n\n", "\n", " "}
					})
					assert(err == nil, "error creating splitter should be nil")
					
					local test_text = "First paragraph with some content.\n\nSecond paragraph with different content.\n\nThird paragraph with more content."
					local chunks, err = splitter:split_text(test_text)
					assert(err == nil, "error splitting text should be nil")
					assert(#chunks > 0, "should have chunks")
					
					return #chunks
				`,
				minChunks: 1,
			},
			{
				name: "empty text",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({chunk_size = 100})
					assert(err == nil, "error creating splitter should be nil")
					
					local chunks, err = splitter:split_text("")
					assert(err == nil, "error splitting empty text should be nil")
					
					return #chunks
				`,
				expectedChunks: 0,
			},
			{
				name: "single word",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({chunk_size = 100})
					assert(err == nil, "error creating splitter should be nil")
					
					local chunks, err = splitter:split_text("word")
					assert(err == nil, "error splitting single word should be nil")
					assert(#chunks == 1, "should have exactly one chunk")
					assert(chunks[1] == "word", "chunk should contain the word")
					
					return #chunks
				`,
				expectedChunks: 1,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTextModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1)
				chunkCount := int(result.(lua.LNumber))

				if tc.expectedChunks > 0 {
					assert.Equal(t, tc.expectedChunks, chunkCount)
				} else if tc.minChunks > 0 {
					assert.GreaterOrEqual(t, chunkCount, tc.minChunks)
				}
				vm.State().Pop(1)
			})
		}
	})

	t.Run("batch splitting functionality", func(t *testing.T) {
		mod := NewTextModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local text = require("text")
			local splitter, err = text.splitter.recursive({
				chunk_size = 50,
				chunk_overlap = 10
			})
			assert(err == nil, "error creating splitter should be nil")
			
			local pages = {
				{
					content = "This is page 1 content with some text that should be split.",
					metadata = {page_number = 1, document_id = "doc123"}
				},
				{
					content = "This is page 2 content with different text that also needs splitting.",
					metadata = {page_number = 2, document_id = "doc123"}
				}
			}
			
			local chunks, err = splitter:split_batch(pages)
			assert(err == nil, "error splitting batch should be nil")
			assert(type(chunks) == "table", "chunks should be a table")
			assert(#chunks > 2, "should have more than 2 chunks due to splitting")
			
			-- Check first chunk structure
			assert(type(chunks[1].content) == "string", "chunk content should be string")
			assert(type(chunks[1].metadata) == "table", "chunk metadata should be table")
			assert(chunks[1].metadata.document_id == "doc123", "metadata should be preserved")
			
			return #chunks
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1)
		chunkCount := int(result.(lua.LNumber))
		assert.Greater(t, chunkCount, 2)
	})

	t.Run("markdown splitting", func(t *testing.T) {
		mod := NewTextModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local text = require("text")
			local splitter, err = text.splitter.markdown({
				chunk_size = 200,
				chunk_overlap = 50,
				heading_hierarchy = true,
				code_blocks = false
			})
			assert(err == nil, "error creating markdown splitter should be nil")
			
			local markdown_text = [[
# Main Header

This is content under the main header.

## Sub Header

This is content under the sub header.

### Another Sub Header

More content here with **bold** and *italic* text.

- List item 1
- List item 2
- List item 3
]]
			
			local chunks, err = splitter:split_text(markdown_text)
			assert(err == nil, "error splitting markdown should be nil")
			assert(#chunks > 0, "should have at least one chunk")
			
			return #chunks
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1)
		chunkCount := int(result.(lua.LNumber))
		assert.Greater(t, chunkCount, 0)
	})

	t.Run("metadata preservation and reuse", func(t *testing.T) {
		mod := NewTextModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local text = require("text")
			local splitter, err = text.splitter.recursive({chunk_size = 30})
			assert(err == nil, "error creating splitter should be nil")
			
			-- Create metadata table with complex nested structure
			local original_metadata = {
				source = "test_doc.txt",
				page = 1,
				nested = {
					tags = {"important", "test"},
					config = {
						version = "1.0",
						options = {debug = true}
					}
				}
			}
			
			local pages = {{
				content = "This is a long text that will definitely be split into multiple chunks because it's longer than the chunk size.",
				metadata = original_metadata
			}}
			
			local chunks, err = splitter:split_batch(pages)
			assert(err == nil, "error splitting batch should be nil")
			assert(#chunks > 1, "should have multiple chunks")
			
			-- Verify that all chunks reference the same original metadata table
			local first_meta = chunks[1].metadata
			assert(first_meta.source == "test_doc.txt", "metadata should be preserved")
			assert(first_meta.nested.tags[1] == "important", "nested metadata should be preserved")
			assert(first_meta.nested.config.options.debug == true, "deeply nested metadata should be preserved")
			
			-- Verify that the metadata table is actually the same reference (not a copy)
			-- This tests our implementation detail of reusing Lua tables
			for i = 2, #chunks do
				assert(chunks[i].metadata == first_meta, "all chunks should reference same metadata table")
			end
			
			return {
				chunk_count = #chunks,
				metadata_preserved = (first_meta.source == "test_doc.txt"),
				nested_preserved = (first_meta.nested.tags[1] == "important")
			}
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)
		chunkCount := int(result.RawGetString("chunk_count").(lua.LNumber))
		metadataPreserved := bool(result.RawGetString("metadata_preserved").(lua.LBool))
		nestedPreserved := bool(result.RawGetString("nested_preserved").(lua.LBool))

		assert.Greater(t, chunkCount, 1)
		assert.True(t, metadataPreserved)
		assert.True(t, nestedPreserved)
	})

	t.Run("large document splitting", func(t *testing.T) {
		mod := NewTextModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local text = require("text")
			local splitter, err = text.splitter.recursive({
				chunk_size = 1000,
				chunk_overlap = 150,
				separators = {"\n\n", "\n", ". ", " ", ""}
			})
			assert(err == nil, "error creating splitter should be nil")
			
			-- Create a large document (similar to your embed use case)
			local large_text = ""
			for i = 1, 200 do
				large_text = large_text .. "This is paragraph " .. i .. " of a large document. "
				large_text = large_text .. "It contains multiple sentences that should be split efficiently. "
				large_text = large_text .. "The text splitter should handle this much better than naive string operations. "
				large_text = large_text .. "Each paragraph has enough content to test the splitting algorithm properly.\n\n"
			end
			
			local chunks, err = splitter:split_text(large_text)
			assert(err == nil, "error splitting large text should be nil")
			assert(#chunks > 10, "should have many chunks for large document")
			
			-- Verify chunk sizes are reasonable
			local total_chars = 0
			for i, chunk in ipairs(chunks) do
				assert(#chunk > 0, "chunk should not be empty")
				total_chars = total_chars + #chunk
			end
			
			return {
				original_length = #large_text,
				chunk_count = #chunks,
				total_chars_in_chunks = total_chars,
				avg_chunk_size = math.floor(total_chars / #chunks)
			}
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)
		originalLength := int(result.RawGetString("original_length").(lua.LNumber))
		chunkCount := int(result.RawGetString("chunk_count").(lua.LNumber))
		avgChunkSize := int(result.RawGetString("avg_chunk_size").(lua.LNumber))

		assert.Greater(t, originalLength, 50000, "should be a large document")
		assert.Greater(t, chunkCount, 50, "should have many chunks")
		assert.Less(t, avgChunkSize, 1200, "average chunk size should be reasonable")
		assert.Greater(t, avgChunkSize, 500, "chunks shouldn't be too small")

		t.Logf("Large document test: %d chars → %d chunks (avg: %d chars/chunk)",
			originalLength, chunkCount, avgChunkSize)
	})

	t.Run("edge cases", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "very small chunk size",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({
						chunk_size = 5,
						chunk_overlap = 1
					})
					assert(err == nil, "error creating splitter should be nil")
					
					local chunks, err = splitter:split_text("This is a test")
					assert(err == nil, "error splitting should be nil")
					assert(#chunks > 0, "should have at least one chunk")
				`,
			},
			{
				name: "zero overlap",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({
						chunk_size = 50,
						chunk_overlap = 0
					})
					assert(err == nil, "error creating splitter should be nil")
					
					local chunks, err = splitter:split_text("This is a longer text that should be split without any overlap between chunks.")
					assert(err == nil, "error splitting should be nil")
					assert(#chunks > 0, "should have chunks")
				`,
			},
			{
				name: "text shorter than chunk size",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({chunk_size = 1000})
					assert(err == nil, "error creating splitter should be nil")
					
					local chunks, err = splitter:split_text("Short text.")
					assert(err == nil, "error splitting should be nil")
					assert(#chunks == 1, "should have exactly one chunk")
					assert(chunks[1] == "Short text.", "chunk should contain original text")
				`,
			},
			{
				name: "unicode and emoji handling",
				script: `
					local text = require("text")
					local splitter, err = text.splitter.recursive({chunk_size = 50})
					assert(err == nil, "error creating splitter should be nil")
					
					local test_text = "Hello 👋 World 🌍! This text contains emojis 😊 and unicode characters like café, naïve, and résumé."
					local chunks, err = splitter:split_text(test_text)
					assert(err == nil, "error splitting unicode should be nil")
					assert(#chunks > 0, "should have chunks")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTextModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("error handling", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			expectedError string
		}{
			{
				name: "invalid text type for splitting",
				script: `
					local text = require("text")
					local splitter, _ = text.splitter.recursive({})
					local chunks, err = splitter:split_text(123)  -- Invalid type
					return chunks, err
				`,
				expectedError: "string expected",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTextModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				if err != nil {
					// Error during execution
					assert.Contains(t, err.Error(), tc.expectedError)
				} else {
					// Check returned error value
					errValue := vm.State().Get(-1)
					if errValue.Type() != lua.LTNil {
						assert.Contains(t, errValue.String(), tc.expectedError)
					}
					vm.State().Pop(2) // Pop both result and error
				}
			})
		}
	})
}
