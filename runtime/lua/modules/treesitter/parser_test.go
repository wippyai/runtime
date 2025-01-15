package treesitter

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParser(t *testing.T) {
	logger := zap.NewNop()

	t.Run("parser creation and basic usage", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			
			-- Test parser creation
			local parser = treesitter.parser()
			assert(parser ~= nil, "parser should not be nil")
			assert(type(parser) == "userdata", "parser should be userdata")

			-- Test setting language
			local ok = parser:set_language("go")
			assert(ok, "should set language successfully")

			-- Test basic parse
			local code = "package main"
			local tree = parser:parse(code)
			assert(tree ~= nil, "tree should not be nil")
			assert(type(tree) == "userdata", "tree should be userdata")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser with timeout", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			parser:set_language("go")
			parser:set_timeout(0.1) -- 100ms timeout
			
			local code = "package main\n\nfunc main() {}\n"
			local tree = parser:parse(code)
			assert(tree ~= nil, "should parse within timeout")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser with old tree", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			parser:set_language("go")

			-- Parse initial code
			local code1 = "package main"
			local tree1 = parser:parse(code1)
			assert(tree1 ~= nil, "first tree should not be nil")

			-- Parse modified code with old tree
			local code2 = "package test"
			local tree2 = parser:parse(code2, tree1)
			assert(tree2 ~= nil, "second tree should not be nil")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser with context deadline", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			parser:set_language("go")
			local tree = parser:parse("package main")
			assert(tree ~= nil, "should parse within deadline")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser errors", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()

			-- Test invalid language
			local ok, err = pcall(function()
				parser:set_language("invalid_lang")
			end)
			assert(not ok, "should fail for invalid language")
			assert(string.match(err, "not found"), "should mention unsupported language")

			-- Test parse without language set
			local ok, err = pcall(function()
				parser:parse("some code")
			end)
			assert(not ok, "should fail when language not set")

			-- Test parse with invalid tree arg type
			parser:set_language("go")
			local ok, err = pcall(function()
				parser:parse("code", "not a tree")
			end)
			assert(not ok, "should fail with invalid tree type" .. err)
			assert(string.match(err, "expected"), "should mention expected:")

			-- Test empty code
			local tree, err = parser:parse("")
			assert(tree ~= nil, "should return tree for empty code")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser reset", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			parser:set_language("go")
			parser:reset()
			
			-- Should still work after reset
			local tree = parser:parse("package main")
			assert(tree ~= nil, "should parse after reset")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser garbage collection", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		cleanup := closer.NewCleanup()
		ctx := context.WithValue(context.Background(), ctxapi.CleanupCtx, cleanup)

		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			parser:set_language("go")
			
			-- Force garbage collection
			parser = nil
			collectgarbage()
		`, "test")
		assert.NoError(t, err)

		// Verify cleanup works with GC
		err = cleanup.Close()
		assert.NoError(t, err)
	})

	t.Run("get language", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
        local treesitter = require("treesitter")
        local parser = treesitter.parser()
        
        -- Test getting language before setting
        local ok, err = pcall(function()
			parser:get_language() -- should raise error
		end)
        assert(not ok, "must fail on no language")
        
        -- Test getting language after setting
        parser:set_language("go")
        lang = parser:get_language()
        assert(lang == "go", "language should be 'go' after setting")
        
        -- Test getting language after reset
        parser:reset()
        lang = parser:get_language()  
        assert(lang == "go", "language should remain 'go' after reset")

        -- Test setting different language
        assert(parser:set_language("javascript"))
        lang = parser:get_language()
        assert(lang == "javascript", "language should be 'javascript' after changing")
    `, "test")
		assert.NoError(t, err)
	})
}

func TestParserWithRangesForNestedCode(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)
	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
        local treesitter = require("treesitter")
        local parser = treesitter.parser()
        
        local code = [[
<html>
    <script type="text/go">
package main

func hello() {
    println("Hello")
}
    </script>
    <script type="text/go">
package main

func world() {
    println("World")
}
    </script>
</html>]]
        
        local function find_script_positions()
            local positions = {}
            local start_pattern = '<script type="text/go">'
            local end_pattern = '</script>'
            
            local pos = 1
            while true do
                local start_tag_start = string.find(code, start_pattern, pos, true)
                if not start_tag_start then break end
                
                local content_start = start_tag_start + #start_pattern + 1
                local end_tag_start = string.find(code, end_pattern, content_start, true)
                if not end_tag_start then break end
                
                while string.match(string.sub(code, content_start, content_start), "%s") do
                    content_start = content_start + 1
                end
                
                local content_end = end_tag_start - 1
                while string.match(string.sub(code, content_end, content_end), "%s") do
                    content_end = content_end - 1
                end
                
                local row = 0
                local col = 0
                for i = 1, content_start - 1 do
                    if string.sub(code, i, i) == '\n' then
                        row = row + 1
                        col = 0
                    else
                        col = col + 1
                    end
                end
                
                local end_row = row
                local end_col = col
                for i = content_start, content_end do
                    if string.sub(code, i, i) == '\n' then
                        end_row = end_row + 1
                        end_col = 0
                    else
                        end_col = end_col + 1
                    end
                end
                
                table.insert(positions, {
                    start_byte = content_start - 1,
                    end_byte = content_end,
                    start_row = row,
                    start_col = col,
                    end_row = end_row,
                    end_col = end_col
                })
                
                pos = end_tag_start + #end_pattern
            end
            return positions
        end
        
        local ranges = find_script_positions()
        parser:set_language("go")
        assert(parser:set_ranges(ranges), "should set ranges successfully")
        
        local tree = parser:parse(code)
        assert(tree, "should parse tree successfully")
        
        local root = tree:root_node()
        assert(root, "should have root node")
        
        local package_count = 0
        for i = 0, root:child_count() - 1 do
            local child = root:child(i)
            if child and child:kind() == "package_clause" then
                package_count = package_count + 1
            end
        end
        
        assert(package_count == 2, "should have found two complete Go files (with package clauses)")
        assert(root:child_count() == 4, "should have four nodes total (2 package clauses + 2 function declarations)")
    `, "test")
	assert.NoError(t, err)
}

func TestParserContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	ctx, cancel := context.WithCancel(context.Background())
	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Cancel before parsing
	cancel()

	err = vm.DoString(ctx, `
        local treesitter = require("treesitter")
        local parser = treesitter.parser()
        parser:set_language("go")
        local tree = parser:parse("package main\n\nfunc main() {\n    // Large function body\n}")
    `, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestParserLifecycle(t *testing.T) {
	logger := zap.NewNop()

	t.Run("parser close and reset", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
            local treesitter = require("treesitter")
            
            -- Create parser and parse some code
            local parser = treesitter.parser()
            parser:set_language("go")
            
            local code1 = "package main\n\nfunc test() {}\n"
            local tree1 = parser:parse(code1)
            assert(tree1 ~= nil, "should parse first code")
            
            -- Test parser reset
            parser:reset()
            
            -- Parser should still work after reset
            local code2 = "package other\n\nfunc other() {}\n"
            local tree2 = parser:parse(code2)
            assert(tree2 ~= nil, "should parse after reset")
            assert(tree2:root_node():text(code2) == code2, "tree should contain new code")
            
            -- Test parser close
            parser:close()
            
            -- Operations after close should fail gracefully
            local ok, err = pcall(function()
                parser:parse("should fail")
            end)
            assert(not ok, "parsing after close should fail")
            
            -- Test double close should not crash
            parser:close()
            
            -- Create new parser to verify we can still create and use parsers
            local parser2 = treesitter.parser()
            parser2:set_language("go")
            local tree3 = parser2:parse("package main")
            assert(tree3 ~= nil, "new parser should work")
        `, "test")
		assert.NoError(t, err)
	})

	t.Run("parser close with memory write", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
            local treesitter = require("treesitter")
            
            -- Test parser memory handling during edits
            local parser = treesitter.parser()
            parser:set_language("go")
            
            local code = "func main() {}\n"
            local tree = parser:parse(code)
            assert(tree ~= nil, "should parse code")
            
            -- Edit the tree
            local edit = {
                start_byte = 0,
                old_end_byte = 4,
                new_end_byte = 4,
                start_row = 0,
                start_column = 0,
                old_end_row = 0,
                old_end_column = 4,
                new_end_row = 0,
                new_end_column = 4
            }
            tree:edit(edit)
            
            -- Close parser mid-operation
            parser:close()
            
            -- Tree should still be valid
            assert(tree:root_node() ~= nil, "tree should still be valid after parser close")
            
            -- But new operations should fail
            local ok, err = pcall(function()
                parser:parse("package main")
            end)
            assert(not ok, "parser should not work after close")
        `, "test")
		assert.NoError(t, err)
	})
}

func TestParserResetAndClose(t *testing.T) {
	logger := zap.NewNop()

	t.Run("parser reset and close", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			
			-- Get a parser and parse some code
			local parser = treesitter.parser()
			assert(parser:set_language("go"), "should set language")
			
			local code1 = "package main\n\nfunc main() {}"
			local tree1 = parser:parse(code1)
			assert(tree1 ~= nil, "should parse first code")
			
			-- Reset the parser
			parser:reset()
			
			-- Parse different code after reset
			local code2 = "package test\n\nfunc Test() {}"
			local tree2 = parser:parse(code2)
			assert(tree2 ~= nil, "should parse second code")
			
			-- Verify trees are different
			local root1 = tree1:root_node()
			local root2 = tree2:root_node()
			assert(root1:text() ~= root2:text(), "trees should have different text")
			
			-- Test parser close
			parser:close()
			
			-- Verify parser operations fail after close
			local ok, err = pcall(function()
				parser:parse("invalid after close")
			end)
			assert(not ok, "parser should fail after close")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("multiple reset cycles", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			assert(parser:set_language("go"), "should set language")
			
			-- Multiple parse-reset cycles
			local codes = {
				"package main\n\nfunc main() {}",
				"package test\n\nfunc Test() {}",
				"package util\n\nfunc Util() {}"
			}
			
			local trees = {}
			for i, code in ipairs(codes) do
				local tree = parser:parse(code)
				assert(tree ~= nil, string.format("should parse code %d", i))
				trees[i] = tree:root_node():text()
				parser:reset()
			end
			
			-- Verify all trees were different
			assert(trees[1] ~= trees[2], "first and second trees should differ")
			assert(trees[2] ~= trees[3], "second and third trees should differ")
			assert(trees[1] ~= trees[3], "first and third trees should differ")
			
			parser:close()
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("reset with different languages", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			
			-- Test with Go
			assert(parser:set_language("go"), "should set Go language")
			local go_tree = parser:parse("package main")
			assert(go_tree ~= nil, "should parse Go code")
			
			parser:reset()
			
			-- Test with JavaScript after reset
			assert(parser:set_language("javascript"), "should set JavaScript language")
			local js_tree = parser:parse("function test() {}")
			assert(js_tree ~= nil, "should parse JavaScript code")
			
			-- Verify languages are different - using node counts
			local go_lang = go_tree:language()
			local js_lang = js_tree:language()
			assert(go_lang:node_kind_count() ~= js_lang:node_kind_count(), 
				"languages should have different characteristics")
			
			parser:close()
		`, "test")
		assert.NoError(t, err)
	})
}
