package yaml

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestYAMLModule(t *testing.T) {
	t.Run("module loading", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			assert(type(yaml) == "table")
			assert(type(yaml.encode) == "function")
			assert(type(yaml.decode) == "function")
		`)
		assert.NoError(t, err)
	})

	t.Run("basic encode/decode", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			
			local data = {
				name = "test",
				version = 1.5,
				enabled = true,
				tags = {"one", "two", "three"}
			}
			
			local encoded, err = yaml.encode(data)
			assert(err == nil, "encoding error: " .. tostring(err))
			
			local decoded, err = yaml.decode(encoded)
			assert(err == nil, "decoding error: " .. tostring(err))
			
			-- Verify values
			assert(decoded.name == "test")
			assert(decoded.version == 1.5)
			assert(decoded.enabled == true)
			assert(#decoded.tags == 3)
			
			return encoded
		`)
		require.NoError(t, err)
		yamlStr := L.ToString(-1)
		assert.Contains(t, yamlStr, "name: test")
	})

	t.Run("multiline strings", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			
			local data = {
				description = "This is a\nmultiline string\nwith several lines"
			}
			
			local encoded, err = yaml.encode(data)
			assert(err == nil, "encoding error: " .. tostring(err))
			
			-- Get raw string for test comparison
			print("YAML Output: " .. encoded)
			
			local decoded, err = yaml.decode(encoded)
			assert(err == nil, "decoding error: " .. tostring(err))
			
			-- Verify the multiline string was preserved
			assert(decoded.description == data.description)
			
			return encoded
		`)
		require.NoError(t, err)
		yamlStr := L.ToString(-1)

		// Just check if we have our key and a pipe character indicating literal style
		assert.True(t, strings.Contains(yamlStr, "description:"))
		assert.True(t, strings.Contains(yamlStr, "|") || strings.Contains(yamlStr, "|-"),
			"Multiline strings should use pipe symbol")
	})

	t.Run("field ordering", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			
			-- Create data with specific order
			local data = {
				c_field = "third",
				a_field = "first",
				b_field = "second"
			}
			
			-- Set field order
			local options = {
				field_order = {"a_field", "b_field", "c_field"}
			}
			
			local encoded, err = yaml.encode(data, options)
			assert(err == nil, "encoding error: " .. tostring(err))
			
			return encoded
		`)
		require.NoError(t, err)

		// Get YAML and check field order
		yamlStr := L.ToString(-1)
		aPos := strings.Index(yamlStr, "a_field:")
		bPos := strings.Index(yamlStr, "b_field:")
		cPos := strings.Index(yamlStr, "c_field:")

		assert.True(t, aPos < bPos, "a_field should come before b_field")
		assert.True(t, bPos < cPos, "b_field should come before c_field")
	})

	t.Run("alphabetical sorting", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			
			-- Create data with non-alphabetical order
			local data = {
				z_field = "last",
				a_field = "first",
				m_field = "middle"
			}
			
			-- Enable sort_unordered
			local options = {
				sort_unordered = true
			}
			
			local encoded, err = yaml.encode(data, options)
			assert(err == nil, "encoding error: " .. tostring(err))
			
			return encoded
		`)
		require.NoError(t, err)

		// Get YAML and check alphabetical order
		yamlStr := L.ToString(-1)
		aPos := strings.Index(yamlStr, "a_field:")
		mPos := strings.Index(yamlStr, "m_field:")
		zPos := strings.Index(yamlStr, "z_field:")

		assert.True(t, aPos < mPos, "a_field should come before m_field")
		assert.True(t, mPos < zPos, "m_field should come before z_field")
	})

	t.Run("style options", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		// This test prints the actual outputs for better debugging
		err := L.DoString(`
			local yaml = require("yaml")
			
			local data = {
				version = "1.0",
				nested = {
					deep = "value",
					array = {"one", "two", "three"}
				},
				multiline = "This is a\nmultiline string\nwith several lines",
				short_list = {1, 2, 3},
				longer_list = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			}
			
			-- Test different style options and print the actual results for debugging
			local results = {}
			
			-- Flow mapping style
			results.flow_mapping = yaml.encode(data, {
				mapping_style = "flow"
			})
			print("FLOW_MAPPING OUTPUT: " .. results.flow_mapping)
			
			-- Literal scalar style 
			results.literal_scalar = yaml.encode(data, {
				scalar_style = "literal"
			})
			print("LITERAL_SCALAR OUTPUT: " .. results.literal_scalar)
			
			-- Flow sequence style
			results.flow_sequence = yaml.encode(data, {
				sequence_style = "flow" 
			})
			print("FLOW_SEQUENCE OUTPUT: " .. results.flow_sequence)
			
			-- Compact sequences
			results.compact = yaml.encode(data, {
				compact_sequences = true
			})
			print("COMPACT OUTPUT: " .. results.compact)
			
			-- Double quoted scalars
			results.double_quoted = yaml.encode(data, {
				scalar_style = "double"
			})
			print("DOUBLE_QUOTED OUTPUT: " .. results.double_quoted)
			
			-- Custom indentation
			results.custom_indent = yaml.encode(data, {
				indent = 4
			})
			print("CUSTOM_INDENT OUTPUT: " .. results.custom_indent)
			
			return results
		`)
		require.NoError(t, err)

		results := L.CheckTable(-1)

		// Flow mapping - just check for a single { character
		flowMapping := lua.LVAsString(results.RawGetString("flow_mapping"))
		assert.True(t, strings.Contains(flowMapping, "{"),
			"Flow mapping style should include a { character")

		// Literal scalar style
		literalScalar := lua.LVAsString(results.RawGetString("literal_scalar"))
		assert.True(t, strings.Contains(literalScalar, "|"),
			"Literal scalar style should include a | character")

		// Flow sequence style
		flowSequence := lua.LVAsString(results.RawGetString("flow_sequence"))
		assert.True(t, strings.Contains(flowSequence, "["),
			"Flow sequence style should include a [ character")

		// Double quoted scalars
		doubleQuoted := lua.LVAsString(results.RawGetString("double_quoted"))
		assert.True(t, strings.Contains(doubleQuoted, "\""),
			"Double quoted style should include double quotes")

		// Custom indent
		customIndent := lua.LVAsString(results.RawGetString("custom_indent"))
		lines := strings.Split(customIndent, "\n")
		foundIndent := false

		// Look for a line with 4-space indentation
		for _, line := range lines {
			if strings.HasPrefix(line, "    ") {
				foundIndent = true
				break
			}
		}
		assert.True(t, foundIndent, "Custom indent should include lines with 4 spaces")
	})

	t.Run("complex nested structures", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			
			local data = {
				version = "1.0",
				namespace = "app.agent.exec",
				entries = {
					{
						name = "exec_agent",
						kind = "registry.entry",
						meta = {
							type = "agent.gen1",
							name = "command-executor",
							title = "Command Executor",
							group = {"System Utilities"},
							comment = "Agent for executing system commands",
							icon = "terminal",
							tags = {"command", "exec", "shell", "terminal"}
						},
						prompt = "Example prompt text",
						model = "gpt-4o",
						max_tokens = 4000,
						temperature = 0.3,
						tools = {"tool1", "tool2"}
					}
				}
			}
			
			-- Define options for nice formatting
			local options = {
				indent = 2,
				field_order = {
					"version",
					"namespace",
					"entries",
					"name",
					"kind",
					"meta",
					"type"
				},
				compact_sequences = true,
				sort_unordered = true
			}
			
			local encoded, err = yaml.encode(data, options)
			assert(err == nil, "encoding error: " .. tostring(err))
			
			local decoded, err = yaml.decode(encoded)
			assert(err == nil, "decoding error: " .. tostring(err))
			
			-- Verify roundtrip values
			assert(decoded.version == "1.0")
			assert(decoded.entries[1].meta.type == "agent.gen1")
			
			return encoded
		`)
		require.NoError(t, err)
		yamlStr := L.ToString(-1)

		// Check field ordering
		versionPos := strings.Index(yamlStr, "version:")
		namespacePos := strings.Index(yamlStr, "namespace:")
		entriesPos := strings.Index(yamlStr, "entries:")

		assert.True(t, versionPos < namespacePos, "version should come before namespace")
		assert.True(t, namespacePos < entriesPos, "namespace should come before entries")
	})

	t.Run("error handling", func(t *testing.T) {
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		err := L.DoString(`
			local yaml = require("yaml")
			
			-- Test encode error (non-table input)
			local encoded, err = yaml.encode("not a table")
			assert(encoded == nil, "result should be nil for error")
			assert(err ~= nil, "error should not be nil for invalid input")
			
			-- Test decode error (invalid YAML)
			local decoded, err = yaml.decode("invalid: : yaml : : content")
			assert(decoded == nil, "result should be nil for error")
			assert(err ~= nil, "error should not be nil for invalid YAML")
			
			return {encode_err = err}
		`)
		require.NoError(t, err)
	})
}
