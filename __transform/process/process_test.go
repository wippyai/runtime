package process

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewProcessModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local process = require("process")
			assert(type(process) == "table")
			assert(type(process.run) == "function")
			assert(type(process.start) == "function")
			assert(type(process.stream) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("run command test cases", func(t *testing.T) {
		testCases := []struct {
			name           string
			script         string
			expectedOutput string
			shouldError    bool
		}{
			{
				name: "echo command",
				script: `
					local process = require("process")
					function test()
						local proc = process.start("echo", "hello world")
						assert(proc ~= nil, "process start failed")
						local exit_code = proc:wait()
						assert(exit_code == 0, "process failed")
						return true
					end
					return test
				`,
				expectedOutput: "true",
				shouldError:    false,
			},
			{
				name: "invalid command",
				script: `
					local process = require("process")
					function test()
						local proc = process.start("nonexistentcommand")
						if proc == nil then
							return false
						end
						local exit_code = proc:wait()
						return exit_code == 0
					end
					return test
				`,
				expectedOutput: "false",
				shouldError:    true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewProcessModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				err = vm.CompileFunction("test", tc.script)
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test")
				if tc.shouldError {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tc.expectedOutput, result.String())
				}
			})
		}
	})

	t.Run("lines iterator test", func(t *testing.T) {
		mod := NewProcessModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local process = require("process")
			function test()
				local result = {}
				local proc = process.start("echo", "line1\nline2\nline3")
				assert(proc ~= nil, "process start failed")
				
				for line in proc:lines("stdout") do
					table.insert(result, line)
				end
				
				proc:wait()
				return table.concat(result, ";")
			end
			return test
		`
		err = vm.CompileFunction("test", script)
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.Equal(t, "line1;line2;line3", result.String())
	})

	t.Run("stream test", func(t *testing.T) {
		mod := NewProcessModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local process = require("process")
			local output = {}
			function test()
				local proc = process.start("echo", "hello\nworld")
				assert(proc ~= nil, "process start failed")
				
				process.stream(proc, function(line, stream)
					table.insert(output, line)
				end)
				
				proc:wait()
				return table.concat(output, ";")
			end
			return test
		`
		err = vm.CompileFunction("test", script)
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.Equal(t, "hello;world", result.String())
	})

	t.Run("process status test", func(t *testing.T) {
		mod := NewProcessModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local process = require("process")
			function test()
				local proc = process.start("sleep", "0.1")
				assert(proc ~= nil, "process start failed")
				
				local status = proc:status()
				assert(status.status == "running", "process should be running")
				
				proc:wait()
				status = proc:status()
				assert(status.status == "terminated", "process should be terminated")
				
				return true
			end
			return test
		`
		err = vm.CompileFunction("test", script)
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.Equal(t, "true", result.String())
	})
}
