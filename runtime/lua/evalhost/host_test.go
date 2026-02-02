package evalhost

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// testLibrarySource is a simple Lua library for testing imports
const testLibrarySource = `
local sdk = {}

sdk.version = "1.0.0"
sdk.name = "TestSDK"

function sdk.greet(name)
    return "Hello, " .. (name or "World") .. "!"
end

function sdk.add(a, b)
    return a + b
end

return sdk
`

// mockImportLoader creates a mock import loader that returns the test library
func mockImportLoader(sources map[registry.ID]string) ImportLoader {
	return func(id registry.ID) (string, error) {
		if source, ok := sources[id]; ok {
			return source, nil
		}
		return "", NewImportError("test", id, nil)
	}
}

func TestHost_Run_WithImports(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider())

	// Configure import loader with test library
	testLibID := registry.ParseID("test.lib:test_sdk")
	host.WithImportLoader(mockImportLoader(map[registry.ID]string{
		testLibID: testLibrarySource,
	}))

	t.Run("access imported library version", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				return {
					get_version = function()
						return sdk.version
					end
				}
			`,
			Method:  "get_version",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		// Result is a Lua string
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "1.0.0", string(lstr))
	})

	t.Run("access imported library name", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				return {
					get_name = function()
						return sdk.name
					end
				}
			`,
			Method:  "get_name",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "TestSDK", string(lstr))
	})

	t.Run("call imported function with default arg", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				return {
					greet_default = function()
						return sdk.greet()
					end
				}
			`,
			Method:  "greet_default",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "Hello, World!", string(lstr))
	})

	t.Run("call imported function with hardcoded arg", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				return {
					greet_test = function()
						return sdk.greet("Test")
					end
				}
			`,
			Method:  "greet_test",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "Hello, Test!", string(lstr))
	})

	t.Run("call imported math function", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				return {
					compute = function()
						return sdk.add(10, 32)
					end
				}
			`,
			Method:  "compute",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		// Result can be LNumber or LInteger depending on Lua version
		switch v := result.(type) {
		case lua.LNumber:
			assert.Equal(t, lua.LNumber(42), v)
		case lua.LInteger:
			assert.Equal(t, lua.LInteger(42), v)
		default:
			t.Fatalf("result should be LNumber or LInteger, got %T", result)
		}
	})

	t.Run("use require to access imported library", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				local my_sdk = require("sdk")
				return {
					get_version = function()
						return my_sdk.version
					end
				}
			`,
			Method:  "get_version",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "1.0.0", string(lstr))
	})

	t.Run("use require to call imported function", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				local my_sdk = require("sdk")
				return {
					greet = function()
						return my_sdk.greet("Wippy")
					end
				}
			`,
			Method:  "greet",
			Modules: []string{},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Equal(t, "Hello, Wippy!", string(lstr))
	})

	t.Run("imports with modules together", func(t *testing.T) {
		result, err := host.Run(context.Background(), RunCmd{
			Source: `
				local my_sdk = require("sdk")
				local json = require("json")
				return {
					encode = function()
						return json.encode({ version = my_sdk.version })
					end
				}
			`,
			Method:  "encode",
			Modules: []string{"json"},
			Imports: map[string]registry.ID{
				"sdk": testLibID,
			},
		})

		require.NoError(t, err)
		lstr, ok := result.(lua.LString)
		require.True(t, ok, "result should be LString, got %T", result)
		assert.Contains(t, string(lstr), "1.0.0")
	})
}

func TestHost_Run_WithImports_NotFound(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider())

	// Configure import loader with empty sources
	host.WithImportLoader(mockImportLoader(map[registry.ID]string{}))

	nonExistentID := registry.ParseID("test.lib:nonexistent")

	_, err := host.Run(context.Background(), RunCmd{
		Source: `
			return { test = function() return sdk.version end }
		`,
		Method:  "test",
		Modules: []string{},
		Imports: map[string]registry.ID{
			"sdk": nonExistentID,
		},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to import")
}

func TestHost_Run_WithImports_NoLoader(t *testing.T) {
	// Host without import loader configured
	host := NewHost(zap.NewNop(), safeModulesProvider())

	testLibID := registry.ParseID("test.lib:test_sdk")

	// Without import loader, imports are silently skipped
	result, err := host.Run(context.Background(), RunCmd{
		Source: `
			return {
				check = function()
					return sdk == nil
				end
			}
		`,
		Method:  "check",
		Modules: []string{},
		Imports: map[string]registry.ID{
			"sdk": testLibID,
		},
	})

	require.NoError(t, err)
	// sdk should be nil because no import loader was configured
	lbool, ok := result.(lua.LBool)
	require.True(t, ok, "result should be LBool, got %T", result)
	assert.Equal(t, lua.LTrue, lbool)
}

func TestHost_Run_WithImports_InvalidSource(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider())

	// Configure import loader with invalid Lua source
	badLibID := registry.ParseID("test.lib:bad_lib")
	host.WithImportLoader(mockImportLoader(map[registry.ID]string{
		badLibID: "this is not valid lua!!!",
	}))

	_, err := host.Run(context.Background(), RunCmd{
		Source: `
			return { test = function() return sdk.version end }
		`,
		Method:  "test",
		Modules: []string{},
		Imports: map[string]registry.ID{
			"sdk": badLibID,
		},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to import")
}
