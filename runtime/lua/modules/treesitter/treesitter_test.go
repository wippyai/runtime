package treesitter

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func assertLua(L *lua.LState) int {
	if L.ToBool(1) {
		return 0
	}
	L.RaiseError(L.OptString(2, "assertion failed!"))
	return 0
}

func TestTreeSitterModule_Parse(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic parse", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			assert(type(tree) == "userdata", "tree should be userdata")
		`, "test")
		assert.NoError(t, err)
	})
}
