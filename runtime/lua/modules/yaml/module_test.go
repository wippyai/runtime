package yaml

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	// Make sure "log" is imported in module.go if you keep the logging code
	// import "log"
)

// --- TestYAMLModule function remains the same as provided previously ---
func TestYAMLModule(t *testing.T) {
	t.Run("module loading", func(t *testing.T) {
		// Create new module
		mod := NewYAMLModule()

		// Create Lua state
		L := lua.NewState()
		defer L.Close()

		// Register module
		L.PreloadModule(mod.Name(), mod.Loader)

		// Test loading
		err := L.DoString(`
			local yaml = require("yaml")
			assert(type(yaml) == "table")
			assert(type(yaml.encode) == "function")
			assert(type(yaml.decode) == "function")
		`)
		assert.NoError(t, err)
	})

	t.Run("encoding with multiline strings", func(t *testing.T) {
		// Create new module
		mod := NewYAMLModule()

		// Create Lua state
		L := lua.NewState()
		defer L.Close()

		// Register module
		L.PreloadModule(mod.Name(), mod.Loader)

		// Test encoding with multiline strings
		err := L.DoString(`
			local yaml = require("yaml")

			local data = {
				name = "get_page",
				kind = "function.lua",
				meta = {
					type = "tool",
					name = "Get Page",
					input_schema = [[{
  "type": "object",
  "properties": {
    "id": {
      "type": "string",
      "description": "The ID of the page"
    }
  },
  "required": ["id"]
}]]
				},
				source = [[
local function handler(request)
    local json = require("json")

    -- Return response
    return {
        status = 200,
        body = json.encode({success = true})
    }
end

return handler
]]
			}

			local result, err = yaml.encode(data)
			if err then
				error("Encoding error: " .. tostring(err))
			end

			-- Check for literal style (|) for multiline strings
			assert(string.find(result, "input_schema: |"), "Multiline input_schema should use literal style")
            assert(string.find(result, "source: |"), "Multiline source should use literal style")

			return result
		`)

		require.NoError(t, err)

		// Get the result from stack
		result := L.ToString(-1)

		// Check for literal style (|) for multiline strings
		assert.Contains(t, result, "input_schema: |")
		assert.Contains(t, result, "source: |") // Add check for source as well
	})

	t.Run("roundtrip encode/decode", func(t *testing.T) {
		// Create new module
		mod := NewYAMLModule()

		// Create Lua state
		L := lua.NewState()
		defer L.Close()

		// Register module
		L.PreloadModule(mod.Name(), mod.Loader)

		// Test roundtrip encoding/decoding
		err := L.DoString(`
			local yaml = require("yaml")

			local original = {
				name = "test",
				version = 1.5,
				enabled = true,
				tags = {"one", "two", "three"},
				nested = {
					foo = "bar",
					count = 42
				}
			}

			-- Encode to YAML
			local yamlStr, err = yaml.encode(original)
			if err then
				error("Encoding error: " .. tostring(err))
			end

			-- Decode back to Lua
			local decoded, err = yaml.decode(yamlStr)
			if err then
				error("Decoding error: " .. tostring(err))
			end

			-- Verify roundtrip (using a helper for deep compare might be better)
			assert(decoded.name == original.name, "name mismatch")
			assert(decoded.version == original.version, "version mismatch")
			assert(decoded.enabled == original.enabled, "enabled mismatch")
			assert(#decoded.tags == #original.tags, "tags length mismatch")
			assert(decoded.tags[1] == original.tags[1], "tags[1] mismatch")
			assert(decoded.tags[2] == original.tags[2], "tags[2] mismatch")
			assert(decoded.tags[3] == original.tags[3], "tags[3] mismatch")
			assert(decoded.nested.foo == original.nested.foo, "nested.foo mismatch")
			assert(decoded.nested.count == original.nested.count, "nested.count mismatch")

			return yamlStr
		`)

		require.NoError(t, err)

		// Get the result YAML from stack
		yamlStr := L.ToString(-1)
		assert.NotEmpty(t, yamlStr)
	})

	t.Run("encoding error handling", func(t *testing.T) {
		// Create new module
		mod := NewYAMLModule()

		// Create Lua state
		L := lua.NewState()
		defer L.Close()

		// Register module
		L.PreloadModule(mod.Name(), mod.Loader)

		// Test error handling when encoding
		err := L.DoString(`
			local yaml = require("yaml")

			-- Try to encode a non-table value
			local result, err = yaml.encode("not a table")

			-- This should fail with an error
			assert(result == nil, "result should be nil for error")
			assert(err ~= nil, "error should not be nil")
			assert(string.find(err, "first argument must be a table"), "wrong error message: " .. tostring(err))

			return err
		`)

		require.NoError(t, err) // DoString itself shouldn't error

		// Get the error message from stack
		errMsg := L.Get(-1) // Get the returned value which is the error string
		require.Equal(t, lua.LTString, errMsg.Type())
		assert.Contains(t, errMsg.String(), "first argument must be a table")
	})

	t.Run("decoding error handling", func(t *testing.T) {
		// Create new module
		mod := NewYAMLModule()

		// Create Lua state
		L := lua.NewState()
		defer L.Close()

		// Register module
		L.PreloadModule(mod.Name(), mod.Loader)

		// Test error handling when decoding
		err := L.DoString(`
			local yaml = require("yaml")

			-- Invalid YAML string
			local result, err = yaml.decode("this is not valid yaml: :")

			-- This should fail with an error
			assert(result == nil, "result should be nil for error")
			assert(err ~= nil, "error should not be nil")
			assert(string.find(err, "error unmarshaling YAML"), "wrong error message: " .. tostring(err))

			return err
		`)

		require.NoError(t, err) // DoString itself shouldn't error

		// Get the error message from stack
		errMsg := L.Get(-1) // Get the returned value which is the error string
		require.Equal(t, lua.LTString, errMsg.Type())
		assert.Contains(t, errMsg.String(), "error unmarshaling YAML")
	})

	t.Run("handling complex nested structures", func(t *testing.T) {
		// Create new module
		mod := NewYAMLModule()

		// Create Lua state
		L := lua.NewState()
		defer L.Close()

		// Register module
		L.PreloadModule(mod.Name(), mod.Loader)

		// Test complex nested structures
		err := L.DoString(`
			local yaml = require("yaml")

			local data = {
				entries = {
					{
						name = "list_pages",
						kind = "function.lua",
						meta = {
							type = "tool",
							name = "List Pages",
							llm_alias = "list_pages",
							description = "Lists all dynamic web pages"
						},
						modules = {"http", "json", "registry"}
					},
					{
						name = "get_page",
						kind = "function.lua",
						meta = {
							type = "tool",
							name = "Get Page",
							llm_alias = "get_page",
							input_schema = [[{
  "type": "object",
  "properties": {
    "id": {"type": "string"}
  }
}]] -- No comma needed here in Lua
						},
						modules = {"http", "json", "registry"}
					}
				},
				version = "1.0",
				namespace = "app.pages"
			}

			local result, err = yaml.encode(data)
			if err then
				error("Encoding error: " .. tostring(err))
			end

			-- Check entry count matches after round trip
			local decoded, err = yaml.decode(result)
			if err then
				error("Decoding error: " .. tostring(err))
			end

			assert(#decoded.entries == #data.entries, "entries count mismatch")
            assert(decoded.entries[2].meta.input_schema == data.entries[2].meta.input_schema, "input_schema mismatch")

			return result
		`)

		require.NoError(t, err)

		// Get the YAML from stack
		yamlStr := L.ToString(-1)

		// Check for literal style in nested structure
		assert.Contains(t, yamlStr, "input_schema: |")
	})
}

// TestFieldOrdering tests the custom field ordering during encoding
func TestFieldOrdering(t *testing.T) {
	t.Run("field ordering name, kind, meta", func(t *testing.T) { // Keep test name
		mod := NewYAMLModule()
		L := lua.NewState()
		defer L.Close()
		L.PreloadModule(mod.Name(), mod.Loader)

		// DO NOT CHANGE THE LUA CODE BELOW EXCEPT FOR THE ASSERTIONS AND string.find assignments
		err := L.DoString(`
            local yaml = require("yaml")

            local data = {
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
                },
                version = "1.0",
                namespace = "app.agent.exec",
                meta = {
                    depends_on = {"ns:system", "ns:app.tools.exec", "ns:app.tools.fs"}
                }
            }

            -- THIS FIELD ORDER LIST MUST NOT BE CHANGED --
            local field_order = {
                "version",     -- 0
                "namespace",   -- 1
                "name",        -- 2  <- Global name
                "kind",        -- 3
                "meta",        -- 4  <- Global meta
                "entries",     -- 5
                "type",        -- 6
                "title",       -- 7
                "comment",     -- 8
                "group",       -- 9
                "depends_on",  -- 10
                "icon",        -- 11
                "tags",        -- 12
                "prompt",      -- 13
                "model",       -- 14
                "max_tokens",  -- 15
                "temperature", -- 16
                "tools",       -- 17
            }
            -- END OF UNCHANGEABLE FIELD ORDER LIST --

            -- Encode with field ordering
            local result, err = yaml.encode(data, field_order)
            if err then
                error("Encoding error: " .. tostring(err))
            end

            -- 1. Verify Top-Level Order (Corrected patterns)
            assert(string.find(result, "^version:") or string.find(result, "\nversion:") , "Top-level version missing")
            assert(string.find(result, "\nnamespace:") , "Top-level namespace missing")
            assert(string.find(result, "\nmeta:") , "Top-level meta missing") -- Check line 73 again
            assert(string.find(result, "\nentries:") , "Top-level entries missing")

            local ver_pos = string.find(result, "^version:") or string.find(result, "\nversion:")
            local ns_pos = string.find(result, "\nnamespace:")
            local meta_pos_top = string.find(result, "\nmeta:") -- Finds the first '\nmeta:', which should be the top-level one
            local entries_pos = string.find(result, "\nentries:")

            assert(ver_pos, "Top-level version position not found")
            assert(ns_pos, "Top-level namespace position not found")
            assert(meta_pos_top, "Top-level meta position not found")
            assert(entries_pos, "Top-level entries position not found")

            -- Assert actual output order: version < namespace < meta < entries
            assert(ver_pos < ns_pos, "Actual order check: version should come before namespace")
            assert(ns_pos < meta_pos_top, "Actual order check: namespace should come before top-level meta")
            assert(meta_pos_top < entries_pos, "Actual order check: top-level meta should come before entries")


            -- 2. Verify Order within 'entries' item based on the *actual output*
            -- Output showed: name < kind < meta < prompt < model < ...

            -- Patterns adjusted for list item structure
            -- Match the line starting with '- ' OR subsequent lines with just indentation
            local name_pos = string.find(result, "\n%s*-%s+name: exec_agent") -- First key after '- '
            local kind_pos = string.find(result, "\n%s+kind: registry%.entry") -- Subsequent key, indented
            local meta_pos_nested_start = string.find(result, "\n%s+meta:") -- Subsequent key, indented, start of block
            local prompt_pos = string.find(result, "\n%s+prompt: Example prompt text") -- Subsequent key, indented
            local model_pos = string.find(result, "\n%s+model: gpt%-4o") -- Subsequent key, indented

            -- Check positions are found before comparing
            assert(name_pos, "'name: exec_agent' line starting with '- ' not found") -- Line 104 check
            assert(kind_pos, "'kind: registry.entry' indented line not found")
            assert(meta_pos_nested_start, "start of nested 'meta:' block (indented) not found")
            assert(prompt_pos, "'prompt: Example prompt text' indented line not found")
            assert(model_pos, "'model: gpt-4o' indented line not found")

            -- Assert actual output order within the entry: name < kind < meta < prompt < model
            assert(name_pos < kind_pos, "Actual order check: name should come before kind in entry item")
            assert(kind_pos < meta_pos_nested_start, "Actual order check: kind should come before nested meta in entry item")
            assert(meta_pos_nested_start < prompt_pos, "Actual order check: nested meta should come before prompt in entry item")
            assert(prompt_pos < model_pos, "Actual order check: prompt should come before model in entry item")


            -- 3. Verify Order within the nested 'meta' object based on the *actual output*
            -- Output showed: name < type < title < comment < group < icon < tags

            -- Search relative to the start of the nested meta block for clarity, using further indentation
            local name_pos_meta = string.find(result, "\n%s+name: command%-executor", meta_pos_nested_start)
            local type_pos_meta = string.find(result, "\n%s+type: agent%.gen1", meta_pos_nested_start)
            local title_pos_meta = string.find(result, "\n%s+title: Command Executor", meta_pos_nested_start)
            local comment_pos_meta = string.find(result, "\n%s+comment: Agent for executing", meta_pos_nested_start)
            local group_pos_meta = string.find(result, "\n%s+group:", meta_pos_nested_start)

            -- Check positions are found before comparing
            assert(name_pos_meta, "'name: command-executor' line not found within nested meta")
            assert(type_pos_meta, "'type: agent.gen1' line not found within nested meta")
            assert(title_pos_meta, "'title: Command Executor' line not found within nested meta")
            assert(comment_pos_meta, "'comment: Agent for executing' line not found within nested meta")
            assert(group_pos_meta, "'group:' line not found within nested meta")

            -- Assert actual output order within nested meta: name < type < title < comment < group
            assert(name_pos_meta < type_pos_meta, "Actual order check: name should come before type in nested meta")
            assert(type_pos_meta < title_pos_meta, "Actual order check: type should come before title in nested meta")
            assert(title_pos_meta < comment_pos_meta, "Actual order check: title should come before comment in nested meta")
            assert(comment_pos_meta < group_pos_meta, "Actual order check: comment should come before group in nested meta")

            return result
        `)

		require.NoError(t, err, "Lua execution failed")

		yamlStr := L.Get(-1)
		require.Equal(t, lua.LTString, yamlStr.Type(), "Lua script did not return a string")
		assert.NotEmpty(t, yamlStr.String())

	})
}
