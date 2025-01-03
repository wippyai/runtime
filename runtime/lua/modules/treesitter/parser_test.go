package treesitter

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
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

		err = vm.DoString(nil, `
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

	t.Run("parser with old tree", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
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

	t.Run("parser errors", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()

			-- Test invalid language
			local ok, err = pcall(function()
				parser:set_language("invalid_lang")
			end)
			assert(not ok, "should fail for invalid language")
			assert(string.match(err, "unsupported language"), "should mention unsupported language")

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
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("parser garbage collection", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local parser = treesitter.parser()
			parser:set_language("go")
			
			-- Force garbage collection
			parser = nil
			collectgarbage()
		`, "test")
		assert.NoError(t, err)
	})
}
