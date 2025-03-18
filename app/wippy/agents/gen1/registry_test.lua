local agent_registry = require("agent_registry")

local function define_tests()
    describe("Agent Registry", function()
        -- Sample agent registry entries for testing
        local agent_entries = {
            ["wippy.agents.gen1:chatter"] = {
                id = "wippy.agents.gen1:chatter",
                kind = "registry.entry",
                meta = {
                    type = "agent.gen1",
                    name = "Conversational Chat Assistant",
                    comment = "A helpful assistant for general conversation and information retrieval."
                },
                data = {
                    prompt = "You are a helpful, friendly assistant...",
                    model = "gpt-4o",
                    max_tokens = 1600,
                    temperature = 0.5,
                    flavors = { "conversational", "thinking_tag_user" },
                    tools = { "system:read_file", "system:write_file" },
                    memory = {
                        "Always greet the user in a friendly manner at the beginning of conversations.",
                        "When working with files, confirm the file path before performing operations."
                    }
                }
            },
            ["wippy.agents:files"] = {
                id = "wippy.agents:files",
                kind = "registry.entry",
                meta = {
                    type = "agent.gen1",
                    name = "File Operations Agent",
                    comment = "Agent specialized in file operations"
                },
                data = {
                    prompt = "You are a file operations specialist...",
                    model = "claude-3-5-sonnet",
                    max_tokens = 4000,
                    temperature = 0.3,
                    flavors = { "detailed" },
                    tools = {
                        "system:read_file",
                        "system:write_file",
                        "system:list_files"
                    },
                    memory = {
                        "Always verify file paths before operations.",
                        "Check for file existence before attempting to read."
                    }
                }
            },
            ["wippy.agents:knowledge_base"] = {
                id = "wippy.agents:knowledge_base",
                kind = "registry.entry",
                meta = {
                    type = "agent.gen1",
                    name = "Knowledge Base Agent",
                    comment = "Agent with knowledge retrieval capabilities"
                },
                data = {
                    prompt = "You have access to a knowledge base...",
                    model = "claude-3-7-sonnet",
                    max_tokens = 8000,
                    temperature = 0.2,
                    flavors = { "detailed", "comprehensive" },
                    tools = {
                        "system:search_kb",
                        "system:retrieve_document"
                    },
                    memory = {
                        "When retrieving information, cite the source.",
                        "For complex questions, break down your search into sub-queries."
                    }
                }
            },
            ["wippy.agents.flavors:conversational"] = {
                id = "wippy.agents.flavors:conversational",
                kind = "registry.entry",
                meta = {
                    type = "agent.flavor",
                    name = "Conversational",
                    comment = "Flavor that makes agents conversational and friendly."
                },
                data = {
                    prompt = "You are a friendly, conversational assistant..."
                }
            },
            ["wippy.agents.flavors:thinking_tag_user"] = {
                id = "wippy.agents.flavors:thinking_tag_user",
                kind = "registry.entry",
                meta = {
                    type = "agent.flavor",
                    name = "Thinking Tag (User)",
                    comment = "Flavor that adds structured thinking tags for user visibility."
                },
                data = {
                    prompt = "When tackling complex problems, use <thinking> tags..."
                }
            }
        }

        local mock_registry

        before_each(function()
            -- Create mock registry for testing
            mock_registry = {
                snapshot = function()
                    return {
                        get = function(id)
                            return agent_entries[id]
                        end,
                        find = function(query)
                            local results = {}

                            for _, entry in pairs(agent_entries) do
                                local matches = true

                                -- Match on kind
                                if query[".kind"] and entry.kind ~= query[".kind"] then
                                    matches = false
                                end

                                -- Match on meta.type
                                if query["meta.type"] and entry.meta.type ~= query["meta.type"] then
                                    matches = false
                                end

                                -- Match on flavor contains
                                if query["*data.flavors"] then
                                    local found = false
                                    local flavor = query["*data.flavors"]

                                    if entry.data.flavors then
                                        for _, f in ipairs(entry.data.flavors) do
                                            if f == flavor then
                                                found = true
                                                break
                                            end
                                        end
                                    end

                                    if not found then
                                        matches = false
                                    end
                                end

                                if matches then
                                    table.insert(results, entry)
                                end
                            end

                            return results
                        end
                    }, nil
                end
            }

            -- Inject mock registry
            package.loaded["registry"] = mock_registry

            -- Reload agent_registry with the mock
            package.loaded["agent_registry"] = nil
            require("agent_registry")
        end)

        after_each(function()
            -- Reset the injected modules
            package.loaded["registry"] = nil
            package.loaded["agent_registry"] = nil
        end)

        it("should find agents by flavor", function()
            local entries = agent_registry.find_by_flavor("conversational")

            expect(#entries).to_equal(1)
            expect(entries[1].id).to_equal("wippy.agents.gen1:chatter")
        end)

        it("should handle agent not found by flavor", function()
            local entries, err = agent_registry.find_by_flavor("nonexistent")

            expect(#entries).to_equal(0)
            expect(err).not_to_be_nil()
            expect(err:match("No agents found")).not_to_be_nil()
        end)

        it("should get an agent by id", function()
            -- Update the test agent entry to include inheritance
            local chatter_entry = agent_entries["wippy.agents.gen1:chatter"]
            chatter_entry.data.inherit = { "wippy.agents:files", "wippy.agents:knowledge_base" }

            local agent, err = agent_registry.get_by_id("wippy.agents.gen1:chatter")

            expect(err).to_be_nil()
            expect(agent).not_to_be_nil()
            expect(agent.id).to_equal("wippy.agents.gen1:chatter")
            expect(agent.name).to_equal("Conversational Chat Assistant")
            expect(agent.model).to_equal("gpt-4o")

            -- Check if tools were properly inherited
            expect(#agent.tools).to_equal(5) -- Should have 5 unique tools

            -- Check if memories were properly inherited
            expect(#agent.memory).to_equal(6) -- Should have 6 unique memories

            -- Check if the original prompt is maintained
            expect(agent.prompt).to_equal("You are a helpful, friendly assistant...")
        end)

        it("should handle agent not found by id", function()
            local agent, err = agent_registry.get_by_id("nonexistent")

            expect(agent).to_be_nil()
            expect(err).not_to_be_nil()
            expect(err:match("No agent found")).not_to_be_nil()
        end)

        it("should maintain order stability in tools and memories", function()
            -- Set up an agent with inherited elements
            local chatter_entry = agent_entries["wippy.agents.gen1:chatter"]
            chatter_entry.data.inherit = { "wippy.agents:files", "wippy.agents:knowledge_base" }

            local agent, err = agent_registry.get_by_id("wippy.agents.gen1:chatter")

            expect(err).to_be_nil()

            -- Check if the original tools come first
            expect(agent.tools[1]).to_equal("system:read_file")
            expect(agent.tools[2]).to_equal("system:write_file")

            -- Check if the original memories come first
            expect(agent.memory[1]).to_equal(
            "Always greet the user in a friendly manner at the beginning of conversations.")
            expect(agent.memory[2]).to_equal(
            "When working with files, confirm the file path before performing operations.")
        end)
    end)
end

return require("test").run_cases(define_tests)
