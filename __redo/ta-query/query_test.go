package ta_query

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestTreeSitterModule_Query(t *testing.T) {
	logger := zap.NewNop()

	t.Run("query Go code", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
			engine.WithGlobalFunction("print", func(l *lua.LState) int {
				top := l.GetTop()
				for i := 1; i <= top; i++ {
					t.Logf("%s", l.Get(i).String())
				}
				return 0
			}),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = [[
				package main
				import "fmt"
				func main() {
					fmt.Println("Hello, World!")
				}
			]]
			local query = [[
				(function_declaration name: (identifier) @function.name)
			]]
			local result, err = treesitter.query("go", code, query)

			-- Check if err is nil before proceeding
			assert(err == nil, "treesitter.query returned an error: " .. tostring(err))

			assert(type(result) == "table")
			assert(#result.results > 0, "result.results should not be empty")

			local found = false
			for i, _ in ipairs(result.results) do
				if result.results[i].kind == "function.name" and result.results[i].match == "main" then
					found = true
					break
				end
			end
			assert(found, "result.results should contain function.name 'main'")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("query JavaScript code with values", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
			engine.WithGlobalFunction("print", func(l *lua.LState) int {
				top := l.GetTop()
				for i := 1; i <= top; i++ {
					t.Logf("%s", l.Get(i).String())
				}
				return 0
			}),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
        local treesitter = require("treesitter")
        local code = [[
            function add(a, b) {
                return a + b;
            }
        ]]
        local query = [[
            (function_declaration
                name: (identifier) @function.name
                parameters: (formal_parameters
                    (identifier) @param.first
                    (identifier) @param.second
                )
            )
        ]]
        local result, err = treesitter.query("js", code, query)

        -- Check if err is nil before proceeding
        assert(err == nil, "treesitter.query returned an error: " .. tostring(err))

        assert(type(result) == "table")
        assert(#result.results > 0, "result.results should not be empty")

        local found = false
        for i, _ in ipairs(result.results) do
            if result.results[i].kind == "function.name" and result.results[i].match == "add" and result.results[i].values["param.first"] == "a" and result.results[i].values["param.second"] == "b" then
                found = true
                break
            end
        end
        assert(found, "result.results should contain correct function.name and parameter values")
    `, "test")
		assert.NoError(t, err)
	})

	t.Run("query with invalid query string", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
			engine.WithGlobalFunction("print", func(l *lua.LState) int {
				top := l.GetTop()
				for i := 1; i <= top; i++ {
					t.Logf("%s", l.Get(i).String())
				}
				return 0
			}),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = "package main"
			local query = "invalid query"
			local result, err = treesitter.query("go", code, query)
			assert(result == nil)
			assert(string.find(err, "failed to create query") ~= nil)
		`, "test")
		assert.NoError(t, err)
	})
}
