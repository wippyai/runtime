package uuid

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestUUIDModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewUUIDModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local uuid = require("uuid")
			assert(type(uuid) == "table")
			assert(type(uuid.v4) == "function")
			assert(type(uuid.v7) == "function")
			assert(type(uuid.v1) == "function")
			assert(type(uuid.v3) == "function")
			assert(type(uuid.v5) == "function")
			assert(type(uuid.validate) == "function")
			assert(type(uuid.version) == "function")
			assert(type(uuid.variant) == "function")
			assert(type(uuid.parse) == "function")
			assert(type(uuid.format) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("v4 generation", func(t *testing.T) {
		mod := NewUUIDModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local uuid = require("uuid")
			function test()
				local id, err = uuid.v4()
				if err then
					return nil, err
				end
				local valid, verr = uuid.validate(id)
				if verr then
					return nil, verr
				end
				local version, verr = uuid.version(id)
				if verr then
					return nil, verr
				end
				return {id = id, valid = valid, version = version}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		id := tbl.RawGetString("id").String()
		valid := tbl.RawGetString("valid").(lua.LBool)
		version := int(tbl.RawGetString("version").(lua.LNumber))

		assert.True(t, bool(valid))
		assert.Equal(t, 4, version)
		_, err = uuid.Parse(id)
		assert.NoError(t, err)
	})

	t.Run("v7 generation and timestamp", func(t *testing.T) {
		mod := NewUUIDModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local uuid = require("uuid")
			function test()
				local id, err = uuid.v7()
				if err then
					return nil, err
				end
				local info, err = uuid.parse(id)
				if err then
					return nil, err
				end
				local valid, verr = uuid.validate(id)
				if verr then
					return nil, verr
				end
				return {
					id = id,
					valid = valid,
					version = info.version,
					timestamp = info.timestamp
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		valid := tbl.RawGetString("valid").(lua.LBool)
		version := int(tbl.RawGetString("version").(lua.LNumber))
		timestamp := int64(tbl.RawGetString("timestamp").(lua.LNumber))

		assert.True(t, bool(valid))
		assert.Equal(t, 7, version)
		assert.InDelta(t, time.Now().Unix(), timestamp, 2)
	})

	t.Run("v3 and v5 namespace tests", func(t *testing.T) {
		testCases := []struct {
			name      string
			fn        string
			version   int
			namespace string
		}{
			{
				name:      "v3",
				fn:        "v3",
				version:   3,
				namespace: "6ba7b810-9dad-11d1-80b4-00c04fd430c8", // DNS namespace
			},
			{
				name:      "v5",
				fn:        "v5",
				version:   5,
				namespace: "6ba7b810-9dad-11d1-80b4-00c04fd430c8", // DNS namespace
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewUUIDModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local uuid = require("uuid")
					function test(ns, name)
						if not ns or not name then
							return nil, "missing required arguments"
						end
						local id, err = uuid.` + tc.fn + `(ns, name)
						if err then
							return nil, err
						end
						local version, verr = uuid.version(id)
						if verr then
							return nil, verr
						end
						return {id = id, version = version}
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				// Test valid namespace
				result, err := vm.Execute(context.Background(), "test",
					lua.LString(tc.namespace),
					lua.LString("test.example.com"))
				require.NoError(t, err)
				require.NotNil(t, result)

				tbl, ok := result.(*lua.LTable)
				require.True(t, ok)
				assert.Equal(t, tc.version, int(tbl.RawGetString("version").(lua.LNumber)))

				// Test deterministic behavior
				result2, err := vm.Execute(context.Background(), "test",
					lua.LString(tc.namespace),
					lua.LString("test.example.com"))
				require.NoError(t, err)
				require.NotNil(t, result2)

				tbl2, ok := result2.(*lua.LTable)
				require.True(t, ok)
				assert.Equal(t,
					tbl.RawGetString("id").String(),
					tbl2.RawGetString("id").String(),
					"namespace UUID generation should be deterministic")

				// Test invalid namespace
				result3, err := vm.Execute(context.Background(), "test",
					lua.LString("invalid-uuid"),
					lua.LString("test.example.com"))

				// We expect either an error or a result with an error message
				if err != nil {
					assert.Contains(t, err.Error(), "invalid namespace UUID")
				} else {
					_, ok := result3.(*lua.LNilType)
					assert.True(t, ok, "expected nil result for invalid namespace")
				}
			})
		}
	})

	t.Run("format options", func(t *testing.T) {
		testCases := []struct {
			name       string
			format     string
			hasHyphens bool
			hasPrefix  bool
		}{
			{
				name:       "standard format",
				format:     "standard",
				hasHyphens: true,
				hasPrefix:  false,
			},
			{
				name:       "simple format",
				format:     "simple",
				hasHyphens: false,
				hasPrefix:  false,
			},
			{
				name:       "urn format",
				format:     "urn",
				hasHyphens: true,
				hasPrefix:  true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewUUIDModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local uuid = require("uuid")
					function test(format)
						local id, err = uuid.v4()
						if err then
							return nil, err
						end
						local formatted, err = uuid.format(id, format)
						if err then
							return nil, err
						end
						return formatted
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test", lua.LString(tc.format))
				require.NoError(t, err)
				require.IsType(t, lua.LString(""), result)

				formatted := result.String()
				if tc.hasHyphens {
					assert.Contains(t, formatted, "-")
				} else {
					assert.NotContains(t, formatted, "-")
				}
				if tc.hasPrefix {
					assert.True(t, strings.HasPrefix(formatted, "urn:uuid:"))
				}
			})
		}
	})

	t.Run("validation tests", func(t *testing.T) {
		testCases := []struct {
			name          string
			input         lua.LValue
			expectValid   bool
			expectError   bool
			errorContains string
		}{
			{
				name:        "valid uuid",
				input:       lua.LString("123e4567-e89b-42d3-a456-426614174000"),
				expectValid: true,
			},
			{
				name:        "invalid format",
				input:       lua.LString("not-a-uuid"),
				expectValid: false,
			},
			{
				name:        "empty string",
				input:       lua.LString(""),
				expectValid: false,
			},
			{
				name:          "non-string input",
				input:         lua.LNumber(42),
				expectValid:   false,
				expectError:   true,
				errorContains: "input must be a string",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewUUIDModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local uuid = require("uuid")
					function test(input)
						local valid, err = uuid.validate(input)
						if err then
							error(err)
						end
						return valid
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test", tc.input)

				if tc.expectError {
					assert.Error(t, err)
					if tc.errorContains != "" {
						assert.Contains(t, err.Error(), tc.errorContains)
					}
					return
				}

				require.NoError(t, err)
				valid, ok := result.(lua.LBool)
				require.True(t, ok)
				assert.Equal(t, tc.expectValid, bool(valid))
			})
		}
	})

	t.Run("parse function", func(t *testing.T) {
		mod := NewUUIDModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local uuid = require("uuid")
			function test()
				local id, err = uuid.v7()
				if err then
					return nil, err
				end
				local info, err = uuid.parse(id)
				if err then
					return nil, err
				end
				return {
					version = info.version,
					variant = info.variant,
					timestamp = info.timestamp
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		version := int(tbl.RawGetString("version").(lua.LNumber))
		variant := tbl.RawGetString("variant").String()
		timestamp := int64(tbl.RawGetString("timestamp").(lua.LNumber))

		assert.Equal(t, 7, version)
		assert.Equal(t, "RFC4122", variant)
		assert.InDelta(t, time.Now().Unix(), timestamp, 2)
	})
	t.Run("error handling edge cases", func(t *testing.T) {
		mod := NewUUIDModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name          string
			script        string
			expectError   bool
			errorContains string
		}{
			{
				name: "v3 missing arguments",
				script: `
                local uuid = require("uuid")
                function test()
                    local id, err = uuid.v3()
                    if err then
                        error(err)
                    end
                    return id
                end
                return test
            `,
				expectError:   true,
				errorContains: "namespace must be a string",
			},
			{
				name: "v3 invalid namespace type",
				script: `
                local uuid = require("uuid")
                function test()
                    local id, err = uuid.v3(123, "test")
                    if err then
                        error(err)
                    end
                    return id
                end
                return test
            `,
				expectError:   true,
				errorContains: "namespace must be a string",
			},
			{
				name: "v5 invalid name type",
				script: `
                local uuid = require("uuid")
                function test()
                    local id, err = uuid.v5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", 123)
                    if err then
                        error(err)
                    end
                    return id
                end
                return test
            `,
				expectError:   true,
				errorContains: "name must be a string",
			},
			{
				name: "version check on invalid uuid",
				script: `
                local uuid = require("uuid")
                function test()
                    local version, err = uuid.version("invalid-uuid")
                    if err then
                        error(err)
                    end
                    return version
                end
                return test
            `,
				expectError:   true,
				errorContains: "invalid UUID format",
			},
			{
				name: "variant check on invalid uuid",
				script: `
                local uuid = require("uuid")
                function test()
                    local variant, err = uuid.variant("invalid-uuid")
                    if err then
                        error(err)
                    end
                    return variant
                end
                return test
            `,
				expectError:   true,
				errorContains: "invalid UUID format",
			},
			{
				name: "format with invalid uuid",
				script: `
                local uuid = require("uuid")
                function test()
                    local formatted, err = uuid.format("invalid-uuid", "simple")
                    if err then
                        error(err)
                    end
                    return formatted
                end
                return test
            `,
				expectError:   true,
				errorContains: "invalid UUID format",
			},
			{
				name: "format with invalid format type",
				script: `
                local uuid = require("uuid")
                function test()
                    local id, err = uuid.v4()
                    if err then error(err) end
                    local formatted, err = uuid.format(id, "unknown")
                    if err then
                        error(err)
                    end
                    return formatted
                end
                return test
            `,
				expectError:   true,
				errorContains: "unsupported format",
			},
			{
				name: "validate non-string input",
				script: `
                local uuid = require("uuid")
                function test()
                    local valid, err = uuid.validate(123)
                    if err then
                        error(err)
                    end
                    return valid
                end
                return test
            `,
				expectError:   true,
				errorContains: "input must be a string",
			},
			{
				name: "parse non-string input",
				script: `
                local uuid = require("uuid")
                function test()
                    local info, err = uuid.parse(123)
                    if err then
                        error(err)
                    end
                    return info
                end
                return test
            `,
				expectError:   true,
				errorContains: "input must be a string",
			},
			{
				name: "parse invalid uuid format",
				script: `
                local uuid = require("uuid")
                function test()
                    local info, err = uuid.parse("invalid-uuid")
                    if err then
                        error(err)
                    end
                    return info
                end
                return test
            `,
				expectError:   true,
				errorContains: "invalid UUID format",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err = vm.Import(tc.script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test")

				if tc.expectError {
					if err == nil {
						t.Error("expected error but got none")
						return
					}
					assert.Contains(t, err.Error(), tc.errorContains)
					return
				}

				assert.NoError(t, err)
				assert.NotNil(t, result)
			})
		}
	})

	t.Run("chained operations", func(t *testing.T) {
		mod := NewUUIDModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local uuid = require("uuid")
			function test()
				-- Generate a v4 UUID and perform multiple operations
				local id, err = uuid.v4()
				if err then
					return nil, err
				end

				local valid, err = uuid.validate(id)
				if err then
					return nil, err
				end
				if not valid then
					return nil, "validation failed"
				end

				local version, err = uuid.version(id)
				if err then
					return nil, err
				end

				local variant, err = uuid.variant(id)
				if err then
					return nil, err
				end

				local simple, err = uuid.format(id, "simple")
				if err then
					return nil, err
				end

				return {
					original = id,
					version = version,
					variant = variant,
					simple = simple
				}
			end
			return test
		`

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		assert.NotEmpty(t, tbl.RawGetString("original").String())
		assert.Equal(t, 4, int(tbl.RawGetString("version").(lua.LNumber)))
		assert.Equal(t, "RFC4122", tbl.RawGetString("variant").String())
		assert.NotContains(t, tbl.RawGetString("simple").String(), "-")
	})
}
