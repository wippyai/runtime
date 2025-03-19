local agent = require("agent")
local prompt = require("prompt")
local llm = require("llm")
local registry = require("agent_registry")
local json = require("json")
local time = require("time")
local env = require("env")

local function define_tests()
    describe("Agent Library", function()
        local mock_llm
        local mock_prompt
        local mock_time

        -- Sample agent specification for testing
        local test_agent_spec = {
            id = "test-agent",
            name = "Test Agent",
            description = "A test agent for unit testing",
            model = "gpt-4o-mini",
            prompt = "You are a test agent designed for unit testing.",
            tools = { "test:calculator" },
            traits = {},
            memory = { "You were created for testing purposes" },
            handouts = {
                {
                    id = "data-analyst-id",
                    name = "Data Analyst",
                    description = "Expert in data analysis"
                }
            }
        }

        -- Create a calculator tool schema
        local calculator_schema = {
            name = "calculator",
            description = "Perform mathematical calculations",
            schema = {
                type = "object",
                properties = {
                    expression = {
                        type = "string",
                        description = "The mathematical expression to evaluate"
                    }
                },
                required = { "expression" }
            }
        }

        before_each(function()
            -- Create mock LLM module
            mock_llm = {
                generate = function(messages, options)
                    -- Check if tool_ids contains "test:calculator"
                    local has_calculator_tool = false
                    if options.tool_ids then
                        for _, tool_id in ipairs(options.tool_ids) do
                            if tool_id == "test:calculator" then
                                has_calculator_tool = true
                                break
                            end
                        end
                    end

                    -- Standard response for normal queries
                    if not has_calculator_tool then
                        return {
                            result = "This is a test response.",
                            tokens = {
                                prompt_tokens = 10,
                                completion_tokens = 5,
                                thinking_tokens = 0,
                                total_tokens = 15
                            },
                            finish_reason = "stop"
                        }
                    else
                        -- If calculator tool is available, simulate tool calling
                        local last_message = messages[#messages]
                        if last_message.role == "user" and last_message.content[1].text:lower():match("calculat") then
                            return {
                                result = "I'll help you calculate that.",
                                tool_calls = {
                                    {
                                        id = "call_123",
                                        name = "test:calculator",
                                        arguments = {
                                            expression = "123 * 456"
                                        }
                                    }
                                },
                                tokens = {
                                    prompt_tokens = 15,
                                    completion_tokens = 10,
                                    thinking_tokens = 0,
                                    total_tokens = 25
                                },
                                finish_reason = "tool_call"
                            }
                        end

                        -- If handout tool is available and detected, simulate handout delegation
                        if options.tool_schemas and options.tool_schemas["handout_data_analyst"] and
                            last_message.role == "user" and
                            last_message.content[1].text:lower():match("analyze") then
                            return {
                                result = "I'll delegate this to our data analyst.",
                                tool_calls = {
                                    {
                                        id = "call_456",
                                        name = "handout_data_analyst",
                                        arguments = {
                                            message = "Please analyze this data"
                                        }
                                    }
                                },
                                tokens = {
                                    prompt_tokens = 20,
                                    completion_tokens = 15,
                                    thinking_tokens = 0,
                                    total_tokens = 35
                                },
                                finish_reason = "tool_call"
                            }
                        end

                        -- Default response if no special condition is met
                        return {
                            result = "This is a test response.",
                            tokens = {
                                prompt_tokens = 10,
                                completion_tokens = 5,
                                thinking_tokens = 0,
                                total_tokens = 15
                            },
                            finish_reason = "stop"
                        }
                    end
                end
            }

            -- Create mock prompt module for testing
            mock_prompt = {
                new = function()
                    local messages = {
                        { role = "system", content = { { type = "text", text = "You are a test agent" } } }
                    }

                    return {
                        add_system = function(self, content)
                            messages[1].content = { { type = "text", text = content } }
                            return self
                        end,
                        add_user = function(self, content)
                            table.insert(messages, {
                                role = "user",
                                content = { { type = "text", text = content } }
                            })
                            return self
                        end,
                        add_assistant = function(self, content)
                            table.insert(messages, {
                                role = "assistant",
                                content = { { type = "text", text = content } }
                            })
                            return self
                        end,
                        add_function_result = function(self, name, result, id)
                            local content = result
                            if type(result) ~= "string" then
                                content = json.encode(result)
                            end

                            table.insert(messages, {
                                role = "function",
                                name = name,
                                content = { { type = "text", text = content } },
                                function_call_id = id
                            })
                            return self
                        end,
                        add_message = function(self, role, content)
                            table.insert(messages, {
                                role = role,
                                content = content
                            })
                            return self
                        end,
                        get_messages = function()
                            return messages
                        end
                    }
                end
            }

            -- Create mock time module for testing
            mock_time = {
                now = function()
                    return {
                        sub = function()
                            return {
                                milliseconds = function() return 5000 end -- 5 seconds
                            }
                        end,
                        unix = function() return 1647622123 end -- Fixed timestamp
                    }
                end
            }

            -- Inject mocks
            agent._llm = mock_llm
            agent._prompt = mock_prompt
            agent._time = mock_time
        end)

        after_each(function()
            -- Cleanup mocks
            agent._llm = nil
            agent._prompt = nil
            agent._time = nil
        end)

        it("should create an agent from specification", function()
            local test_agent = agent.new(test_agent_spec)

            expect(test_agent).not_to_be_nil()
            expect(test_agent.id).to_equal("test-agent")
            expect(test_agent.name).to_equal("Test Agent")
            expect(test_agent.model).to_equal("gpt-4o-mini")
            expect(test_agent.available_for_next_execute).to_be_true()
        end)

        it("should handle basic conversation", function()
            local test_agent = agent.new(test_agent_spec)

            -- Add a user message
            test_agent:add_user_message("Hello")
            expect(test_agent.messages_handled).to_equal(1)

            -- Execute the agent
            local result = test_agent:step()

            -- Verify result
            expect(result).not_to_be_nil()
            expect(result.result).to_equal("This is a test response.")

            -- Check token tracking
            expect(test_agent.total_tokens.prompt).to_equal(10)
            expect(test_agent.total_tokens.completion).to_equal(5)
            expect(test_agent.total_tokens.total).to_equal(15)

            -- Add the response to conversation
            test_agent:add_assistant_message(result.result)

            -- Get conversation statistics
            local stats = test_agent:get_stats()
            expect(stats.id).to_equal("test-agent")
            expect(stats.messages_handled).to_equal(1)
        end)

        it("should handle tool calls", function()
            local test_agent = agent.new(test_agent_spec)

            -- Add a user message that will trigger tool calling
            test_agent:add_user_message("Calculate 123 * 456")

            -- Execute the agent
            local result = test_agent:step()

            -- Verify tool calls
            expect(result.tool_calls).not_to_be_nil()
            expect(#result.tool_calls).to_equal(1)
            expect(result.tool_calls[1].name).to_equal("test:calculator")
            expect(result.tool_calls[1].arguments.expression).to_equal("123 * 456")

            -- Agent should not be available until tool result is provided
            expect(test_agent.available_for_next_execute).to_be_false()

            -- Add function result
            test_agent:add_function_result(
                "test:calculator",
                { result = 56088 },
                result.tool_calls[1].id
            )

            -- Agent should now be available for next execution
            expect(test_agent.available_for_next_execute).to_be_true()

            -- Execute again to get response with tool result
            local final_result = test_agent:step()
            expect(final_result.result).not_to_be_nil()
        end)

        it("should handle handout delegation", function()
            local test_agent = agent.new(test_agent_spec)

            -- Add a user message that will trigger handout
            test_agent:add_user_message("Analyze this dataset for me")

            -- Execute the agent
            local result = test_agent:step()

            -- Verify handout information
            expect(result.handout_target).not_to_be_nil()
            expect(result.handout_target).to_equal("data-analyst-id")
            expect(result.handout_message).to_equal("Please analyze this data")

            -- Handout tool calls should be intercepted (replaced with handout info)
            expect(result.tool_calls).to_be_nil()
        end)

        it("should clear conversation history", function()
            local test_agent = agent.new(test_agent_spec)

            -- Add some messages and get responses
            test_agent:add_user_message("Hello")
            local result1 = test_agent:step()
            test_agent:add_assistant_message(result1.result)

            test_agent:add_user_message("How are you?")
            local result2 = test_agent:step()
            test_agent:add_assistant_message(result2.result)

            -- Verify message count
            expect(test_agent.messages_handled).to_equal(2)

            -- Clear history
            test_agent:clear_history()

            -- Messages handled should be reset, but token count preserved
            expect(test_agent.messages_handled).to_equal(0)
            expect(test_agent.total_tokens.total > 0).to_be_true()

            -- Should be able to add new messages
            test_agent:add_user_message("After clearing")
            expect(test_agent.messages_handled).to_equal(1)
            expect(test_agent.available_for_next_execute).to_be_true()
        end)

        -- Integration test with real LLM
        it("should integrate with real LLM when not mocked", function()
            -- Skip if agent still has mocked llm
            if agent._llm then return end

            -- Create real agent
            local test_agent = agent.new(test_agent_spec)

            -- Add a simple user message
            test_agent:add_user_message("Hello, what are you?")

            -- Execute with error handling
            local result, err = test_agent:step()
            if err then
                print("Integration test error: " .. err)
                return
            end

            -- Output should contain something about being a test agent
            expect(result).not_to_be_nil()
            expect(result.result).not_to_be_nil()
            expect(result.result:lower():match("test agent")).not_to_be_nil()
        end)
    end)

    describe("Agent Real Integration", function()
        if not env.get("ENABLE_INTEGRATION_TESTS") then
            return
        end

        -- Clear any mocks before integration tests
        before_all(function()
            agent._llm = nil
            agent._prompt = nil
            agent._time = nil
        end)

        -- Real integration test with gpt-4o-mini
        it("should perform conversation with real gpt-4o-mini", function()
            local real_agent_spec = {
                id = "integration-test-agent",
                name = "Integration Test Agent",
                description = "An agent for real integration testing",
                model = "gpt-4o-mini",
                prompt = "You are a helpful assistant designed for integration testing. You are concise but helpful.",
                tools = {},
                traits = {},
                memory = {}
            }

            -- Create agent
            local test_agent = agent.new(real_agent_spec)
            expect(test_agent).not_to_be_nil()

            -- Run a simple conversation
            test_agent:add_user_message("Tell me a very short joke about programming")
            local result = test_agent:step()

            -- Verify response
            expect(result).not_to_be_nil()
            expect(result.result).not_to_be_nil()
            expect(#result.result > 10).to_be_true() -- Ensure there's a real response

            -- Display result for debugging
            print("GPT-4o-mini response: " .. result.result)

            -- Get and print stats
            local stats = test_agent:get_stats()
            print("Messages handled: " .. stats.messages_handled)
            print("Total tokens: " .. stats.total_tokens.total)

            -- Continue conversation
            test_agent:add_assistant_message(result.result)
            test_agent:add_user_message("Explain why that joke is funny in two sentences.")

            local result2 = test_agent:step()
            expect(result2).not_to_be_nil()
            expect(result2.result).not_to_be_nil()

            -- Display result for debugging
            print("Follow-up response: " .. result2.result)
        end)

        -- Integration test with tool calling
        it("should handle tool calls with real model", function()
            -- Get API keys from environment
            local openai_api_key = env.get("OPENAI_API_KEY")
            if not openai_api_key or #openai_api_key < 10 then
                print("Skipping tool calling test: No valid OpenAI API key found")
                return
            end

            -- Define calculator tool schema
            local calculator_schema = {
                name = "calculator",
                description = "Perform mathematical calculations",
                schema = {
                    type = "object",
                    properties = {
                        expression = {
                            type = "string",
                            description = "The mathematical expression to evaluate"
                        }
                    },
                    required = { "expression" }
                }
            }

            -- Create agent
            local test_agent = agent.new({
                id = "calculator-test-agent",
                name = "Calculator Test Agent",
                description = "An agent for testing tool calls",
                model = "gpt-4o-mini",
                prompt =
                "You are a helpful assistant that can perform calculations. Always use the calculator tool for math problems.",
                tools = {}, -- Empty tools list - we'll use inline tools instead
                traits = {},
                memory = {}
            })

            -- Register the calculator tool directly
            test_agent:register_tool("calculator", calculator_schema)

            -- Calculator function handler
            local function handle_calculator_tool(expression)
                -- Simple calculator that evaluates basic math expressions
                if expression == "123 * 456" then
                    return { result = 56088 }
                elseif expression == "123 + 456" then
                    return { result = 579 }
                elseif expression == "123 - 456" then
                    return { result = -333 }
                elseif expression == "456 / 123" then
                    return { result = 3.7073 }
                else
                    return { error = "Expression not supported in test calculator" }
                end
            end

            -- Run a conversation with tool use
            test_agent:add_user_message("What is 123 * 456?")

            -- Process conversation with tools
            local result, err = test_agent:step()
            expect(err).to_be_nil()

            -- If no tool calls, just print the response and skip the rest
            if not result.tool_calls or #result.tool_calls == 0 then
                print("No tool calls made. Response: " .. (result.result or "None"))
                return
            end

            -- Handle tool calls
            test_agent:add_assistant_message(result.result)

            for _, tool_call in ipairs(result.tool_calls) do
                print("Tool called: " .. tool_call.name)

                test_agent:add_function_call(tool_call.name, tool_call.arguments, tool_call.id)

                -- Process the calculator call
                local tool_result
                if tool_call.name == "calculator" then
                    tool_result = handle_calculator_tool(tool_call.arguments.expression)
                else
                    tool_result = { error = "Unknown tool" }
                end

                -- Add the result back to conversation
                test_agent:add_function_result(tool_call.name, tool_result, tool_call.id)
            end

            -- Get final response with tool output incorporated
            local final_result, err = test_agent:step()
            expect(err).to_be_nil("Error processing conversation")
            expect(final_result).not_to_be_nil()
            expect(final_result.result).not_to_be_nil()

            -- Response should contain the calculation result (56088)
            print("Final response: " .. final_result.result)
            expect(final_result.result:match("56088") or
                final_result.result:match("56,088")).not_to_be_nil()
        end)

        -- Integration test with handout delegation
        --it("should handle handout delegation with real model", function()
        --    -- Get API keys from environment
        --    local openai_api_key = env.get("OPENAI_API_KEY")
        --    if not openai_api_key or #openai_api_key < 10 then
        --        print("Skipping handout test: No valid OpenAI API key found")
        --        return
        --    end
        --
        --    -- Create parent agent spec
        --    local parent_agent_spec = {
        --        id = "parent-agent",
        --        name = "Parent Agent",
        --        description = "An agent that can delegate tasks to specialized agents",
        --        model = "gpt-4o-mini",
        --        prompt =
        --        "You are a helpful assistant that coordinates tasks. When you receive requests related to data analysis or technical information, delegate them to the appropriate specialized agent.",
        --        tools = {},
        --        traits = {},
        --        memory = {},
        --        handouts = {
        --            {
        --                id = "data-analyst-id",
        --                name = "Data Analyst",
        --                description = "Expert in data analysis and statistics"
        --            },
        --            {
        --                id = "tech-expert-id",
        --                name = "Tech Expert",
        --                description = "Expert in technical information and programming"
        --            }
        --        }
        --    }
        --
        --    -- Create child agent specs
        --    local data_analyst_spec = {
        --        id = "data-analyst-id",
        --        name = "Data Analyst",
        --        description = "An expert data analyst",
        --        model = "gpt-4o-mini",
        --        prompt =
        --        "You are a data analyst expert. You specialize in analyzing data and explaining statistical concepts.",
        --        tools = {},
        --        traits = {},
        --        memory = {}
        --    }
        --
        --    local tech_expert_spec = {
        --        id = "tech-expert-id",
        --        name = "Tech Expert",
        --        description = "An expert in technical information",
        --        model = "gpt-4o-mini",
        --        prompt =
        --        "You are a technical expert. You specialize in programming, system architecture, and technical problem-solving.",
        --        tools = {},
        --        traits = {},
        --        memory = {}
        --    }
        --
        --    -- Create a simple registry to manage agents
        --    local test_registry = {
        --        agents = {},
        --        register = function(self, agent_spec)
        --            local test_agent = agent.new(agent_spec)
        --            self.agents[agent_spec.id] = test_agent
        --            return test_agent
        --        end,
        --        get = function(self, agent_id)
        --            return self.agents[agent_id]
        --        end
        --    }
        --
        --    -- Register all agents
        --    local parent_agent = test_registry:register(parent_agent_spec)
        --    local data_analyst = test_registry:register(data_analyst_spec)
        --    local tech_expert = test_registry:register(tech_expert_spec)
        --
        --    -- Function to process a conversation with handouts
        --    local function process_conversation(message)
        --        print("User: " .. message)
        --
        --        -- Send message to parent agent
        --        parent_agent:add_user_message(message)
        --        local result, err = parent_agent:step()
        --
        --        print(json.encode(result))
        --        print(json.encode(err))
        --        expect(err).to_be_nil("Error processing conversation")
        --        expect(result).not_to_be_nil("Result is nil")
        --        expect(result.handout_target).not_to_be_nil("Handout target not set")
        --
        --        -- Check for handout delegation
        --        if result.handout_target then
        --            print("Parent delegating to: " .. result.handout_target)
        --            print("Delegation message: " .. result.handout_message)
        --
        --            -- Get the target agent
        --            local target_agent = test_registry:get(result.handout_target)
        --            expect(target_agent).not_to_be_nil()
        --
        --            -- Process the handout
        --            target_agent:add_user_message(result.handout_message)
        --            local specialist_result = target_agent:step()
        --
        --            -- Return to parent with specialist response
        --            print("Specialist response: " .. specialist_result.result)
        --
        --            -- Add the specialist's response back to parent
        --            parent_agent:add_assistant_message("I delegated your question to " ..
        --                target_agent.name .. " and they responded: " .. specialist_result.result)
        --
        --            return {
        --                delegated = true,
        --                original_response = result.result,
        --                target = result.handout_target,
        --                specialist_response = specialist_result.result
        --            }
        --        else
        --            -- No delegation occurred
        --            print("Parent response: " .. result.result)
        --            parent_agent:add_assistant_message(result.result)
        --
        --            return {
        --                delegated = false,
        --                response = result.result
        --            }
        --        end
        --    end
        --
        --    -- Test data analysis question - should be delegated to data analyst
        --    local data_result = process_conversation("Can you explain what correlation coefficient means in statistics?")
        --    expect(data_result.delegated).to_be_true()
        --    expect(data_result.target).to_equal("data-analyst-id")
        --
        --    -- Test technical question - should be delegated to tech expert
        --    local tech_result = process_conversation("What are the key differences between Python and JavaScript?")
        --    expect(tech_result.delegated).to_be_true()
        --    expect(tech_result.target).to_equal("tech-expert-id")
        --
        --    -- Test general question - should NOT be delegated
        --    local general_result = process_conversation("What's the weather like today?")
        --    expect(general_result.delegated).to_be_false()
        --
        --    -- Print results summary
        --    print("\nHandout Test Results:")
        --    print("Data analysis question delegated: " .. tostring(data_result.delegated))
        --    print("Technical question delegated: " .. tostring(tech_result.delegated))
        --    print("General question handled directly: " .. tostring(not general_result.delegated))
        --end)
    end)
end

return require("test").run_cases(define_tests)
