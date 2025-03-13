local text_generation = require("text_generation")
local claude_client = require("claude_client")
local output = require("output")
local json = require("json")
local env = require("env")
local prompt = require("prompt")
local time = require("time") -- Added missing time module

local function define_tests()
    -- Toggle to enable/disable real API integration test
    local RUN_INTEGRATION_TESTS = env.get("ENABLE_INTEGRATION_TESTS")

    describe("Claude Text Generation Handler", function()
        local actual_api_key = nil

        before_all(function()
            -- Check if we have a real API key for integration tests
            actual_api_key = env.get("ANTHROPIC_API_KEY")

            if RUN_INTEGRATION_TESTS then
                if actual_api_key and #actual_api_key > 10 then
                    print("Integration tests will run with real API key")
                else
                    print("Integration tests disabled - no valid API key found")
                    RUN_INTEGRATION_TESTS = false
                end
            else
                print("Integration tests disabled - set ENABLE_INTEGRATION_TESTS=true to enable")
            end
        end)

        it("should successfully generate text with mocked client", function()
            -- Create a mock client constructor
            local mock_client = {
                send_request = function(self, endpoint_path, payload, options)
                    -- Validate the request
                    expect(endpoint_path).to_equal(claude_client.API_ENDPOINTS.MESSAGES)
                    expect(payload.model).to_equal("claude-3-5-haiku-20241022")

                    -- Check messages array
                    local found_user_message = false
                    for _, msg in ipairs(payload.messages) do
                        if msg.role == "user" then
                            if type(msg.content) == "table" then
                                for _, content in ipairs(msg.content) do
                                    if content.type == "text" and content.text == "Say hello world" then
                                        found_user_message = true
                                        break
                                    end
                                end
                            elseif type(msg.content) == "string" and msg.content == "Say hello world" then
                                found_user_message = true
                            end
                        end
                    end
                    expect(found_user_message).to_be_true("Expected user message with 'Say hello world' content")

                    -- Return mock successful response
                    return {
                        content = {
                            {
                                type = "text",
                                text = "Hello, world!"
                            }
                        },
                        id = "msg_mock123",
                        model = "claude-3-5-haiku-20241022",
                        role = "assistant",
                        stop_reason = "end_turn",
                        stop_sequence = nil,
                        type = "message",
                        usage = {
                            input_tokens = 10,
                            output_tokens = 5
                        }
                    }
                end,

                -- Add configure method to prevent "attempt to call a nil value" errors
                configure = function(self, options)
                    return self
                end
            }

            -- Store original function and mock claude_client.new
            local original_new = claude_client.new
            mock(claude_client, "new", function()
                return mock_client
            end)

            -- Create proper prompt using the prompt builder
            local promptBuilder = prompt.new()
            promptBuilder:add_user("Say hello world")

            -- Call with a properly built prompt
            local response = text_generation.handler({
                model = "claude-3-5-haiku-20241022",
                messages = promptBuilder:get_messages()
            })

            -- Verify the response structure
            expect(response.error).to_be_nil("Expected no error")
            expect(response.result).to_equal("Hello, world!")
            expect(response.tokens).not_to_be_nil("Expected token information")
            expect(response.tokens.prompt_tokens).to_equal(10)
            expect(response.tokens.completion_tokens).to_equal(5)
            expect(response.tokens.total_tokens).to_equal(15)
            expect(response.finish_reason).to_equal("stop")
        end)

        --
        --it("should connect to real Claude API with claude-3-5-haiku model", function()
        --    -- Skip test if integration tests are disabled
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create proper prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("Reply with exactly the text 'Integration test successful'")
        --
        --    -- Make a real API call with claude-3-5-haiku
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            temperature = 0, -- Deterministic output
        --            max_tokens = 15  -- Short response
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify response
        --    expect(response.error).to_be_nil("API request failed: " ..
        --        (response.error_message or "unknown error"))
        --    expect(response.result).not_to_be_nil("No response received from API")
        --
        --    -- Should contain our expected phrase
        --    expect(response.result:find("Integration test successful")).not_to_be_nil(
        --        "Expected phrase not found in response: " .. response.result
        --    )
        --
        --    -- Should have token usage
        --    expect(response.tokens).not_to_be_nil("No token usage information received")
        --    expect(response.tokens.prompt_tokens > 0).to_be_true("No prompt tokens reported")
        --    expect(response.tokens.completion_tokens > 0).to_be_true("No completion tokens reported")
        --    expect(response.tokens.total_tokens > 0).to_be_true("No total tokens reported")
        --end)
        --
        --it("should handle wrong model errors correctly with mocked client", function()
        --    -- Mock the client send_request function to simulate a model error
        --    mock(claude_client, "send_request", function(self, endpoint_path, payload, options)
        --        -- Return a model-related error with the correct error type from real API
        --        return nil, {
        --            status_code = 404,
        --            message = "The model 'nonexistent-model' does not exist or you do not have access to it."
        --        }
        --    end)
        --
        --    -- Create proper prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("This is a test message")
        --
        --    -- Call with a non-existent model
        --    local response = text_generation.handler({
        --        model = "nonexistent-model",
        --        messages = promptBuilder:get_messages()
        --    })
        --
        --    -- Verify the mapped error type
        --    expect(response.error).to_equal(output.ERROR_TYPE.MODEL_ERROR)
        --    expect(response.error_message).to_contain("does not exist")
        --end)
        --
        --it("should handle wrong model errors correctly with real API", function()
        --    -- Skip test if integration tests are disabled
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create proper prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("This is an integration test message")
        --
        --    -- Call with a deliberately incorrect model name
        --    local response = text_generation.handler({
        --        model = "nonexistent-model",
        --        messages = promptBuilder:get_messages(),
        --        api_key = actual_api_key -- Use the real API key
        --    })
        --
        --    -- Verify error handling with real API
        --    expect(response.error).to_equal(output.ERROR_TYPE.MODEL_ERROR)
        --    expect(response.error_message).to_contain("does not exist")
        --
        --    -- Log the actual error message for debugging
        --    print("Integration error message: " .. (response.error_message or "nil"))
        --end)
        --
        ---- Test for length stop reason
        --it("should handle length stop reason correctly with mocked client", function()
        --    -- Mock the client send_request function
        --    mock(claude_client, "send_request", function(self, endpoint_path, payload, options)
        --        -- Return a response with max_tokens as finish reason
        --        return {
        --            content = {
        --                {
        --                    type = "text",
        --                    text = "Here's a comprehensive response that will aim"
        --                }
        --            },
        --            id = "msg_mock456",
        --            model = "claude-3-5-haiku-20241022",
        --            role = "assistant",
        --            stop_reason = "max_tokens",
        --            stop_sequence = nil,
        --            type = "message",
        --            usage = {
        --                input_tokens = 10,
        --                output_tokens = 8
        --            }
        --        }
        --    end)
        --
        --    -- Create prompt
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("Generate a long response that should hit the max tokens limit")
        --
        --    -- Call with a small max_tokens setting
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            max_tokens = 8 -- Deliberately small to trigger max_tokens
        --        }
        --    })
        --
        --    -- Verify finish reason mapping
        --    expect(response.finish_reason).to_equal(output.FINISH_REASON.LENGTH)
        --    expect(response.result).to_equal("Here's a comprehensive response that will aim")
        --end)
        --
        ---- Test for context length exceeded error
        --it("should handle context length exceeded error with mocked client", function()
        --    -- Mock the client send_request function to simulate context length error
        --    mock(claude_client, "send_request", function(self, endpoint_path, payload, options)
        --        -- Return nil and an error for context length exceeded
        --        return nil, {
        --            status_code = 400,
        --            message =
        --            "This model's maximum context length is 128000 tokens. However, your message resulted in 130000 tokens. Please reduce the length of the messages."
        --        }
        --    end)
        --
        --    -- Mock the map_error function to properly handle context length errors
        --    mock(claude_client, "map_error", function(err)
        --        -- Check if the error message contains "context length"
        --        if err and err.message and err.message:match("context length") then
        --            return {
        --                error = output.ERROR_TYPE.CONTEXT_LENGTH,
        --                error_message = err.message
        --            }
        --        end
        --
        --        -- Default error handling
        --        return {
        --            error = output.ERROR_TYPE.SERVER_ERROR,
        --            error_message = err.message or "Unknown error"
        --        }
        --    end)
        --
        --    -- Create a prompt builder with a very large content
        --    local promptBuilder = prompt.new()
        --
        --    -- Add a large user message
        --    local largeMessage = string.rep("This is a test message to exceed the context length. ", 6000)
        --    promptBuilder:add_user(largeMessage)
        --
        --    -- Call with the large message
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages()
        --    })
        --
        --    -- Verify the error type
        --    expect(response.error).to_equal(output.ERROR_TYPE.CONTEXT_LENGTH)
        --    expect(response.error_message).to_contain("maximum context length")
        --end)
        --
        ---- Integration test for length finish reason with real API
        --it("should handle length finish reason correctly with real API", function()
        --    -- Skip if not running integration tests
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create prompt
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user(
        --        "Write a detailed explanation of quantum computing that is at least 100 sentences long. Make sure to cover quantum bits, quantum gates, quantum entanglement, quantum algorithms, quantum supremacy, and the future of quantum computing.")
        --
        --    -- Call with a very small max_tokens to ensure we hit the length limit
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            max_tokens = 15, -- Very small to ensure we hit length limit
        --            temperature = 0  -- For consistency
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify no error
        --    expect(response.error).to_be_nil("API request failed: " ..
        --        (response.error_message or "unknown error"))
        --
        --    -- Check tokens usage
        --    expect(response.tokens).not_to_be_nil("Expected token information")
        --
        --    -- Verify tokens are close to our requested max
        --    expect(response.tokens.completion_tokens <= 20).to_be_true("Expected completion tokens near our max")
        --
        --    -- Verify finish reason is length
        --    expect(response.finish_reason).to_equal(output.FINISH_REASON.LENGTH)
        --end)
        --
        ---- Test for extended thinking
        --it("should correctly handle extended thinking with claude-3-7-sonnet", function()
        --    -- Skip if not running integration tests
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create prompt for a reasoning task
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user(
        --        "Solve this step by step: If a train travels at 60 mph for 2.5 hours, then slows down to 40 mph for 1.5 hours, what is the total distance traveled?")
        --
        --    -- Call with thinking enabled
        --    local response = text_generation.handler({
        --        model = "claude-3-7-sonnet-20250219", -- Use Claude 3.7 Sonnet
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            thinking_effort = 20, -- Enable thinking
        --            temperature = 0       -- For consistent results
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify no error
        --    expect(response.error).to_be_nil("API request failed: " ..
        --        (response.error_message or "unknown error"))
        --
        --    -- Verify response has token information
        --    expect(response.tokens).not_to_be_nil("Expected token information")
        --    expect(response.tokens.prompt_tokens > 0).to_be_true("Expected non-zero prompt tokens")
        --    expect(response.tokens.completion_tokens > 0).to_be_true("Expected non-zero completion tokens")
        --
        --    -- Verify we got a reasonable answer containing the correct numbers
        --    expect(response.result).to_contain("150") -- 60 mph * 2.5 hours = 150 miles
        --    expect(response.result).to_contain("60")  -- 40 mph * 1.5 hours = 60 miles
        --    expect(response.result).to_contain("210") -- 150 + 60 = 210 miles
        --end)
        --
        ---- Mocked test for extended thinking
        --it("should correctly handle extended thinking with mocked client", function()
        --    -- Mock the client send_request function
        --    mock(claude_client, "send_request", function(self, endpoint_path, payload, options)
        --        -- Verify that thinking is enabled for Claude 3.7 models
        --        if payload.model:match("claude%-3%-7") or payload.model:match("claude%-3%.7") then
        --            expect(payload.thinking).not_to_be_nil("Expected thinking to be enabled")
        --            expect(payload.thinking.type).to_equal("enabled")
        --            expect(payload.thinking.budget_tokens >= 1024).to_be_true("Expected thinking budget >= 1024")
        --        else
        --            -- For non-3.7 models, thinking should not be set
        --            expect(payload.thinking).to_be_nil("Thinking should not be enabled for non-3.7 models")
        --        end
        --
        --        -- Return a mock response with thinking
        --        return {
        --            content = {
        --                {
        --                    type = "text",
        --                    text =
        --                    "To solve this problem, I'll calculate the distance traveled in each segment and add them together.\n\nFor the first segment:\nDistance = Speed × Time\nDistance = 60 mph × 2.5 hours = 150 miles\n\nFor the second segment:\nDistance = 40 mph × 1.5 hours = 60 miles\n\nTotal distance = 150 miles + 60 miles = 210 miles"
        --                }
        --            },
        --            id = "msg_thinking123",
        --            model = payload.model,
        --            role = "assistant",
        --            stop_reason = "end_turn",
        --            stop_sequence = nil,
        --            type = "message",
        --            usage = {
        --                input_tokens = 30,
        --                output_tokens = 80
        --            }
        --        }
        --    end)
        --
        --    -- Create prompt
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user(
        --        "Solve this step by step: If a train travels at 60 mph for 2.5 hours, then slows down to 40 mph for 1.5 hours, what is the total distance traveled?")
        --
        --    -- Call with thinking enabled for Claude 3.7
        --    local response = text_generation.handler({
        --        model = "claude-3-7-sonnet-20250219", -- Model that supports thinking
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            thinking_effort = 20 -- Enable thinking
        --        }
        --    })
        --
        --    -- Verify the response structure
        --    expect(response.error).to_be_nil("Expected no error")
        --    expect(response.result).to_contain("210 miles")
        --
        --    -- Verify token usage info
        --    expect(response.tokens.prompt_tokens).to_equal(30)
        --    expect(response.tokens.completion_tokens).to_equal(80)
        --    expect(response.tokens.total_tokens).to_equal(110)
        --
        --    -- Now test with a model that doesn't support thinking
        --    local response2 = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022", -- Model that doesn't support thinking
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            thinking_effort = 20 -- Enable thinking, but it should be ignored
        --        }
        --    })
        --
        --    -- Verify still successful
        --    expect(response2.error).to_be_nil("Expected no error with unsupported model")
        --    expect(response2.result).to_contain("210 miles")
        --end)
        --
        --it("should handle developer messages correctly with mocked client", function()
        --    -- Mock the client send_request function
        --    mock(claude_client, "send_request", function(self, endpoint_path, payload, options)
        --        -- Validate the request
        --        expect(endpoint_path).to_equal(claude_client.API_ENDPOINTS.MESSAGES)
        --        expect(payload.model).to_equal("claude-3-5-haiku-20241022")
        --
        --        -- Developer messages should be in system content now
        --        expect(payload.system).not_to_be_nil("Expected system content for developer messages")
        --        local found_dev_content = false
        --
        --        for _, block in ipairs(payload.system) do
        --            if block.type == "text" and block.text:match("concise answer") then
        --                found_dev_content = true
        --                break
        --            end
        --        end
        --
        --        expect(found_dev_content).to_be_true("Developer message not found in system content")
        --
        --        -- Return mock successful response
        --        return {
        --            content = {
        --                {
        --                    type = "text",
        --                    text = "Paris"
        --                }
        --            },
        --            id = "msg_devmsg123",
        --            model = "claude-3-5-haiku-20241022",
        --            role = "assistant",
        --            stop_reason = "end_turn",
        --            stop_sequence = nil,
        --            type = "message",
        --            usage = {
        --                input_tokens = 15,
        --                output_tokens = 1
        --            }
        --        }
        --    end)
        --
        --    -- Create prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("What is the capital of France?")
        --    promptBuilder:add_developer("Provide a concise answer")
        --
        --    -- Call with the properly built prompt
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages()
        --    })
        --
        --    -- Verify the response structure
        --    expect(response.error).to_be_nil("Expected no error")
        --    expect(response.result).to_equal("Paris")
        --    expect(response.tokens).not_to_be_nil("Expected token information")
        --    expect(response.tokens.prompt_tokens).to_equal(15)
        --    expect(response.tokens.completion_tokens).to_equal(1)
        --    expect(response.tokens.total_tokens).to_equal(16)
        --    expect(response.finish_reason).to_equal("stop")
        --end)
        --
        --it("should follow developer message language instructions with real API", function()
        --    -- Skip test if integration tests are disabled
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create proper prompt using the prompt builder with language-specific instruction
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("What is the capital of France?")
        --    promptBuilder:add_developer("Reply in Spanish only, keep it short")
        --
        --    -- Make a real API call
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            temperature = 0, -- Deterministic output
        --            max_tokens = 20  -- Short response
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify response
        --    expect(response.error).to_be_nil("API request failed: " ..
        --        (response.error_message or "unknown error"))
        --    expect(response.result).not_to_be_nil("No response received from API")
        --
        --    -- Check that the response contains Spanish text (common Spanish words)
        --    local spanish_words = { "París", "es", "la", "capital", "de", "Francia" }
        --    local is_spanish = false
        --    for _, word in ipairs(spanish_words) do
        --        if response.result:lower():find(word:lower()) then
        --            is_spanish = true
        --            break
        --        end
        --    end
        --
        --    expect(is_spanish).to_be_true("Response does not appear to be in Spanish: " .. response.result)
        --end)
        --
        --it("should handle streaming text generation with mocked client", function()
        --    -- Set up process.send mock to capture streamed responses
        --    local received_messages = {}
        --    mock(process, "send", function(pid, topic, data)
        --        table.insert(received_messages, { pid = pid, topic = topic, data = data })
        --        -- Print for debugging
        --        print("Received message: " .. json.encode(data))
        --    end)
        --
        --    -- Create a mock stream response
        --    local event_data = {
        --        message_start = {
        --            type = "message_start",
        --            message = {
        --                id = "msg_stream123",
        --                type = "message",
        --                role = "assistant",
        --                content = {},
        --                model = "claude-3-5-haiku-20241022",
        --                stop_reason = nil,
        --                stop_sequence = nil,
        --                usage = { input_tokens = 25, output_tokens = 1 }
        --            }
        --        },
        --        content_block_start = {
        --            type = "content_block_start",
        --            index = 0,
        --            content_block = { type = "text", text = "" }
        --        },
        --        content_block_delta1 = {
        --            type = "content_block_delta",
        --            index = 0,
        --            delta = { type = "text_delta", text = "Hello" }
        --        },
        --        content_block_delta2 = {
        --            type = "content_block_delta",
        --            index = 0,
        --            delta = { type = "text_delta", text = ", " }
        --        },
        --        content_block_delta3 = {
        --            type = "content_block_delta",
        --            index = 0,
        --            delta = { type = "text_delta", text = "world!" }
        --        },
        --        content_block_stop = {
        --            type = "content_block_stop",
        --            index = 0
        --        },
        --        message_delta = {
        --            type = "message_delta",
        --            delta = { stop_reason = "end_turn", stop_sequence = nil },
        --            usage = { output_tokens = 15 }
        --        },
        --        message_stop = {
        --            type = "message_stop"
        --        }
        --    }
        --
        --    -- Mock the process_stream function
        --    mock(claude_client, "process_stream", function(stream_response, callbacks)
        --        -- Call the callbacks in sequence to simulate streaming
        --        callbacks.on_content("Hello")
        --        callbacks.on_content(", ")
        --        callbacks.on_content("world!")
        --
        --        -- Call the done callback at the end
        --        callbacks.on_done({
        --            content = "Hello, world!",
        --            finish_reason = "end_turn",
        --            usage = {
        --                input_tokens = 25,
        --                output_tokens = 15
        --            }
        --        })
        --
        --        -- Return the full content
        --        return "Hello, world!", nil, {
        --            content = "Hello, world!",
        --            finish_reason = "end_turn",
        --            usage = {
        --                input_tokens = 25,
        --                output_tokens = 15
        --            }
        --        }
        --    end)
        --
        --    -- Mock the send_request function to return a streamable response
        --    mock(claude_client, "send_request", function(self, endpoint_path, payload, options)
        --        -- Validate the request has streaming enabled
        --        expect(options.stream).to_be_true("Stream option should be enabled")
        --
        --        -- Return a mock streaming response
        --        return {
        --            status_code = 200,
        --            stream = {}, -- Just needs to exist
        --            headers = {
        --                ["x-request-id"] = "req_streamtest123",
        --                ["processing-ms"] = "200"
        --            },
        --            metadata = {
        --                request_id = "req_streamtest123",
        --                processing_ms = 200
        --            }
        --        }
        --    end)
        --
        --    -- Create proper prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("Tell me a greeting")
        --
        --    -- Call with streaming enabled
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        stream = {
        --            reply_to = "test-process-id",
        --            topic = "test_stream"
        --        }
        --    })
        --
        --    -- Verify the response structure for streaming
        --    expect(response.error).to_be_nil("Expected no error")
        --    expect(response.streaming).to_be_true("Response should indicate streaming")
        --    expect(response.result).to_equal("Hello, world!")
        --
        --    -- Check for content messages
        --    local content_count = 0
        --    local done_count = 0
        --
        --    for i, msg in ipairs(received_messages) do
        --        expect(msg.pid).to_equal("test-process-id")
        --        expect(msg.topic).to_equal("test_stream")
        --
        --        if msg.data.type == "content" then
        --            content_count = content_count + 1
        --        elseif msg.data.type == "done" then
        --            done_count = done_count + 1
        --            -- Check metadata in done message
        --            expect(msg.data.meta.model).to_equal("claude-3-5-haiku-20241022")
        --            expect(msg.data.meta.provider).to_equal("anthropic")
        --        end
        --    end
        --
        --    expect(content_count > 0).to_be_true("Should have content messages")
        --    expect(done_count).to_equal(1, "Should have exactly one done message")
        --end)
        --
        --it("should handle streaming text generation with real Claude API", function()
        --    -- Skip test if integration tests are disabled
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Set up process.send mock to capture streamed responses
        --    local received_messages = {}
        --    mock(process, "send", function(pid, topic, data)
        --        table.insert(received_messages, { pid = pid, topic = topic, data = data })
        --        -- Print for debugging
        --    end)
        --
        --    -- Create prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user(
        --        "Summarize the advantages of streaming LLM responses in exactly 3 bullet (use •) points. Keep it short and concise.")
        --
        --    -- Call with streaming enabled and real API key
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        options = {
        --            temperature = 0, -- Deterministic output
        --            max_tokens = 150 -- Moderate response size
        --        },
        --        api_key = actual_api_key,
        --        stream = {
        --            reply_to = "integration-test-pid",
        --            topic = "integration_stream_test",
        --            buffer_size = 10 -- Small buffer to ensure multiple chunks
        --        }
        --    })
        --
        --    -- Verify the response structure for streaming
        --    expect(response.error).to_be_nil("API request failed: " ..
        --        (response.error_message or "unknown error"))
        --    expect(response.streaming).to_be_true("Response should indicate streaming")
        --    expect(response.result).not_to_be_nil("Should have complete response content")
        --
        --    -- Verify streamed messages
        --    expect(#received_messages > 0).to_be_true("Should have received stream messages")
        --
        --    -- Check for content messages and done message
        --    local content_count = 0
        --    local done_count = 0
        --    local complete_text = ""
        --
        --    for _, msg in ipairs(received_messages) do
        --        expect(msg.pid).to_equal("integration-test-pid")
        --        expect(msg.topic).to_equal("integration_stream_test")
        --
        --        if msg.data.type == "content" then
        --            content_count = content_count + 1
        --            complete_text = complete_text .. (msg.data.content or "")
        --        elseif msg.data.type == "done" then
        --            done_count = done_count + 1
        --            -- Check minimal metadata in done message
        --            expect(msg.data.meta.model).to_equal("claude-3-5-haiku-20241022")
        --            expect(msg.data.meta.provider).to_equal("anthropic")
        --        end
        --    end
        --
        --    expect(content_count > 0).to_be_true("Should have received content messages")
        --    expect(done_count).to_equal(1, "Should have exactly one done message")
        --
        --    -- Verify the complete text has the bullet points we asked for
        --    expect(complete_text:find("•")).not_to_be_nil("Response should contain bullet points")
        --
        --    -- Count bullet points (• or - or * followed by space)
        --    local bullet_count = 0
        --    for _ in complete_text:gmatch("[•%-*]%s") do
        --        bullet_count = bullet_count + 1
        --    end
        --    expect(bullet_count >= 3).to_be_true("Response should have at least 3 bullet points")
        --
        --    -- Final response from handler should match assembled content from stream
        --    expect(response.result).to_equal(complete_text)
        --end)
        --
        --it("should handle cache control with real API", function()
        --    -- Skip if not running integration tests
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create a system prompt with cacheable content
        --    local system_content = "You are an AI assistant tasked with providing concise answers."
        --
        --    -- Create proper prompt using the prompt builder
        --    local promptBuilder = prompt.new()
        --    promptBuilder:add_user("What's 25 times 32?")
        --
        --    -- First call with cache marker - this will create the cache
        --    local response1 = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        system = system_content,
        --        options = {
        --            temperature = 0,    -- Deterministic output
        --            cache_marker = true -- Enable caching
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify response
        --    expect(response1.error).to_be_nil("API request failed: " ..
        --        (response1.error_message or "unknown error"))
        --    expect(response1.result).not_to_be_nil("No response received from API")
        --
        --    -- Check for cache creation tokens
        --    expect(response1.tokens).not_to_be_nil("No token information")
        --    -- If cache was created, cache_creation_input_tokens should be present
        --    -- (but might be 0 if below minimum cacheable size)
        --
        --    -- Wait briefly to ensure cache is available
        --    time.sleep("1s")
        --
        --    -- Second call with same system prompt and cache marker - should use cache
        --    local response2 = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        system = system_content,
        --        options = {
        --            temperature = 0,    -- Deterministic output
        --            cache_marker = true -- Enable caching
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify response
        --    expect(response2.error).to_be_nil("API request failed: " ..
        --        (response2.error_message or "unknown error"))
        --    expect(response2.result).not_to_be_nil("No response received from API")
        --
        --    -- Check for cache read tokens (might not appear in all models or scenarios)
        --    expect(response2.tokens).not_to_be_nil("No token information")
        --
        --    -- Print cache token information for debugging
        --    print("First call cache info:")
        --    if response1.tokens and response1.tokens.cache_creation_input_tokens then
        --        print("Cache creation tokens: " .. response1.tokens.cache_creation_input_tokens)
        --    else
        --        print("No cache creation tokens found - prompt may be below minimum cacheable size")
        --    end
        --
        --    print("Second call cache info:")
        --    if response2.tokens and response2.tokens.cache_read_input_tokens then
        --        print("Cache read tokens: " .. response2.tokens.cache_read_input_tokens)
        --    else
        --        print("No cache read tokens found - cache may not have been used")
        --    end
        --end)
        --
        --it("should respect system prompts when generating responses", function()
        --    -- Skip test if integration tests are disabled
        --    if not RUN_INTEGRATION_TESTS then
        --        print("Skipping system prompt integration test - not enabled")
        --        return
        --    end
        --
        --    -- Create a prompt with a clear system instruction
        --    local promptBuilder = prompt.new()
        --    local system_prompt =
        --    "You must respond in the style of a pirate captain. Use pirate language, sayings like 'Arrr' and 'Ahoy', and talk about the sea."
        --
        --    promptBuilder:add_user("Tell me about coding best practices")
        --
        --    -- Make the real API call
        --    local response = text_generation.handler({
        --        model = "claude-3-5-haiku-20241022",
        --        messages = promptBuilder:get_messages(),
        --        system = system_prompt,
        --        options = {
        --            temperature = 0, -- Deterministic output
        --            max_tokens = 150 -- Moderate response size
        --        },
        --        api_key = actual_api_key
        --    })
        --
        --    -- Verify response
        --    expect(response.error).to_be_nil("API request failed")
        --    expect(response.result).not_to_be_nil("No response received from API")
        --
        --    -- Check for pirate language markers in the response
        --    local pirate_markers = { "arr", "ahoy", "matey", "sea", "ship", "pirate", "captain" }
        --    local has_pirate_language = false
        --    for _, marker in ipairs(pirate_markers) do
        --        if response.result:lower():find(marker) then
        --            has_pirate_language = true
        --            break
        --        end
        --    end
        --
        --    expect(has_pirate_language).to_be_true(
        --        "Response doesn't contain pirate language as instructed by system message: " .. response.result)
        --
        --    -- Print response for manual verification
        --    print("System prompt test response: " .. response.result:sub(1, 100) .. "...")
        --end)
    end)
end

return require("test").run_cases(define_tests)
