local text_generation = require("text_generation")
local openai = require("openai_client")
local output = require("output")
local json = require("json")
local env = require("env")

-- Define tests
local function define_tests()
    describe("OpenAI Text Generation Handler", function()
        -- Check if we should run integration tests
        local RUN_INTEGRATION_TESTS = env.get("ENABLE_OPENAI_INTEGRATION_TESTS") or true
        local actual_api_key = nil

        before_all(function()
            -- Check if we have a real API key for integration tests
            actual_api_key = env.get("OPENAI_API_KEY")

            if RUN_INTEGRATION_TESTS then
                if actual_api_key and #actual_api_key > 10 then
                    print("Integration tests will run with real API key")
                else
                    print("Integration tests disabled - no valid API key found")
                    RUN_INTEGRATION_TESTS = false
                end
            else
                print("Integration tests disabled - set ENABLE_OPENAI_INTEGRATION_TESTS=true to enable")
            end
        end)

        -- Setup mocks before each test
        before_each(function()
            -- Mock openai.request for unit tests
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate the request
                expect(endpoint_path).to_equal(openai.DEFAULT_CHAT_ENDPOINT)
                expect(payload.model).not_to_be_nil()
                expect(payload.messages).not_to_be_nil()

                -- Return mock successful response
                return {
                    choices = {
                        {
                            message = {
                                content = "This is a test response"
                            },
                            finish_reason = "stop"
                        }
                    },
                    usage = {
                        prompt_tokens = 10,
                        completion_tokens = 5,
                        total_tokens = 15,
                        completion_tokens_details = {
                            reasoning_tokens = 2
                        }
                    },
                    metadata = {
                        request_id = "req_mocktest123",
                        processing_ms = 150
                    }
                }
            end)
        end)

        it("should require a model parameter", function()
            -- Call without a model
            local response = text_generation.handler({
                message = "Test message"
            })

            -- Should return an error response
            expect(response.error).not_to_be_nil()
            expect(response.error).to_equal(output.ERROR_TYPE.INVALID_REQUEST)
            expect(response.error_message).to_equal("Model is required")
        end)

        it("should require messages", function()
            -- Call without messages or message
            local response = text_generation.handler({
                model = "gpt-4"
            })

            -- Should return an error response
            expect(response.error).not_to_be_nil()
            expect(response.error_message).to_equal("No messages provided")
        end)

        it("should handle single message input", function()
            -- Setup specific check for this test
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate the message formatting
                expect(#payload.messages).to_equal(1)
                expect(payload.messages[1].role).to_equal("user")
                expect(payload.messages[1].content).to_equal("Test message")

                return {
                    choices = { { message = { content = "Response" }, finish_reason = "stop" } },
                    usage = { prompt_tokens = 10, completion_tokens = 5, total_tokens = 15 }
                }
            end)

            -- Call with a single message
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message"
            })

            -- Should succeed
            expect(response.error).to_be_nil()
            expect(response.result).to_equal("Response")
        end)

        it("should handle system prompt and user message", function()
            -- Setup specific check for this test
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate the message formatting
                expect(#payload.messages).to_equal(2)
                expect(payload.messages[1].role).to_equal("system")
                expect(payload.messages[1].content).to_equal("You are a helpful assistant")
                expect(payload.messages[2].role).to_equal("user")
                expect(payload.messages[2].content).to_equal("Test message")

                return {
                    choices = { { message = { content = "Response" }, finish_reason = "stop" } },
                    usage = { prompt_tokens = 15, completion_tokens = 5, total_tokens = 20 }
                }
            end)

            -- Call with system prompt and user message
            local response = text_generation.handler({
                model = "gpt-4",
                system_prompt = "You are a helpful assistant",
                message = "Test message"
            })

            -- Should succeed
            expect(response.error).to_be_nil()
            expect(response.result).to_equal("Response")
        end)

        it("should handle direct messages array", function()
            -- Setup specific check for this test
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate the message formatting
                expect(#payload.messages).to_equal(3)
                expect(payload.messages[1].role).to_equal("system")
                expect(payload.messages[2].role).to_equal("user")
                expect(payload.messages[3].role).to_equal("assistant")

                return {
                    choices = { { message = { content = "Response" }, finish_reason = "stop" } },
                    usage = { prompt_tokens = 25, completion_tokens = 5, total_tokens = 30 }
                }
            end)

            -- Call with direct messages array
            local response = text_generation.handler({
                model = "gpt-4",
                messages = {
                    { role = "system",    content = "You are a helpful assistant" },
                    { role = "user",      content = "Hello" },
                    { role = "assistant", content = "Hi there! How can I help you?" }
                }
            })

            -- Should succeed
            expect(response.error).to_be_nil()
            expect(response.result).to_equal("Response")
        end)

        it("should pass through model parameters correctly", function()
            -- Setup specific check for this test
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate parameters
                expect(payload.model).to_equal("gpt-4")
                expect(payload.temperature).to_equal(0.7)
                expect(payload.top_p).to_equal(0.9)
                expect(payload.max_tokens).to_equal(100)
                expect(payload.presence_penalty).to_equal(0.1)
                expect(payload.frequency_penalty).to_equal(0.2)

                return {
                    choices = { { message = { content = "Response" }, finish_reason = "stop" } },
                    usage = { prompt_tokens = 10, completion_tokens = 5, total_tokens = 15 }
                }
            end)

            -- Call with parameters
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message",
                temperature = 0.7,
                top_p = 0.9,
                max_tokens = 100,
                presence_penalty = 0.1,
                frequency_penalty = 0.2
            })

            -- Should succeed
            expect(response.error).to_be_nil()
            expect(response.result).to_equal("Response")
        end)

        it("should handle thinking_effort parameter for reasoning models", function()
            -- Setup specific check for this test
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate reasoning parameter
                expect(payload.reasoning_effort).to_equal(0.5)

                return {
                    choices = { { message = { content = "Response" }, finish_reason = "stop" } },
                    usage = {
                        prompt_tokens = 10,
                        completion_tokens = 5,
                        total_tokens = 15,
                        completion_tokens_details = {
                            reasoning_tokens = 3
                        }
                    }
                }
            end)

            -- Call with thinking_effort
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message",
                thinking_effort = 0.5
            })

            -- Should succeed
            expect(response.error).to_be_nil()
            expect(response.result).to_equal("Response")
            expect(response.tokens).not_to_be_nil()
            expect(response.tokens.thinking_tokens).to_equal(3)
        end)

        it("should handle JSON mode", function()
            -- Setup specific check for this test
            mock(openai, "request", function(endpoint_path, payload, options)
                -- Validate JSON mode
                expect(payload.response_format).not_to_be_nil()
                expect(payload.response_format.type).to_equal("json_object")

                return {
                    choices = { { message = { content = '{"result":"success"}' }, finish_reason = "stop" } },
                    usage = { prompt_tokens = 10, completion_tokens = 5, total_tokens = 15 }
                }
            end)

            -- Call with JSON mode
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Return a JSON object",
                response_format = "json"
            })

            -- Should succeed with JSON content
            expect(response.error).to_be_nil()
            expect(response.result).to_equal('{"result":"success"}')
        end)

        it("should handle API request errors", function()
            -- Mock an API error
            mock(openai, "request", function(endpoint_path, payload, options)
                return nil, {
                    type = output.ERROR_TYPE.RATE_LIMIT,
                    message = "Rate limit exceeded",
                    status_code = 429
                }
            end)

            -- Call with parameters
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message"
            })

            -- Should return error
            expect(response.error).to_equal(output.ERROR_TYPE.RATE_LIMIT)
            expect(response.error_message).to_equal("Rate limit exceeded")
            expect(response.status_code).to_equal(429)
        end)

        it("should handle invalid response structure", function()
            -- Mock a bad response
            mock(openai, "request", function(endpoint_path, payload, options)
                return {
                    -- Missing choices array
                    usage = { prompt_tokens = 10, completion_tokens = 5, total_tokens = 15 }
                }
            end)

            -- Call with parameters
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message"
            })

            -- Should return error
            expect(response.error).to_equal(output.ERROR_TYPE.SERVER_ERROR)
            expect(response.error_message).to_equal("Invalid response structure from OpenAI")
        end)

        it("should handle response with no content", function()
            -- Mock a response with empty content
            mock(openai, "request", function(endpoint_path, payload, options)
                return {
                    choices = {
                        {
                            message = {},  -- No content field
                            finish_reason = "stop"
                        }
                    },
                    usage = { prompt_tokens = 10, completion_tokens = 0, total_tokens = 10 }
                }
            end)

            -- Call with parameters
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message"
            })

            -- Should return error
            expect(response.error).to_equal(output.ERROR_TYPE.SERVER_ERROR)
            expect(response.error_message).to_equal("No content in OpenAI response")
        end)

        it("should track token usage correctly", function()
            -- Mock response with token usage
            mock(openai, "request", function(endpoint_path, payload, options)
                return {
                    choices = {
                        {
                            message = { content = "Response text" },
                            finish_reason = "stop"
                        }
                    },
                    usage = {
                        prompt_tokens = 25,
                        completion_tokens = 15,
                        total_tokens = 40,
                        completion_tokens_details = {
                            reasoning_tokens = 10
                        }
                    }
                }
            end)

            -- Call with parameters
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Test message"
            })

            -- Should return token counts
            expect(response.error).to_be_nil()
            expect(response.tokens).not_to_be_nil()
            expect(response.tokens.prompt_tokens).to_equal(25)
            expect(response.tokens.completion_tokens).to_equal(15)
            expect(response.tokens.total_tokens).to_equal(40)
            expect(response.tokens.thinking_tokens).to_equal(10)
        end)

        -- Integration tests - only run if enabled
        it("should connect to OpenAI API and get a response", function()
            -- Skip test if integration tests are disabled
            if not RUN_INTEGRATION_TESTS then
                print("Skipping integration test - not enabled")
                return
            end

            -- Restore original functions for integration test
            restore_all_mocks()

            -- Make a real API call
            local response = text_generation.handler({
                model = "gpt-3.5-turbo", -- Use a cheaper model for tests
                message = "Reply with exactly the text 'Integration test successful'",
                temperature = 0,         -- Deterministic output
                max_tokens = 15          -- Short response
            })

            -- Verify response
            expect(response.error).to_be_nil("API request failed: " ..
                (response.error_message or "unknown error"))
            expect(response.result).not_to_be_nil("No response received from API")

            -- Should contain our expected phrase
            expect(response.result:find("Integration test successful")).not_to_be_nil(
                "Expected phrase not found in response: " .. response.result
            )

            -- Should have token usage
            expect(response.tokens).not_to_be_nil("No token usage information received")
            expect(response.tokens.prompt_tokens > 0).to_be_true("No prompt tokens reported")
            expect(response.tokens.completion_tokens > 0).to_be_true("No completion tokens reported")
            expect(response.tokens.total_tokens > 0).to_be_true("No total tokens reported")

            -- Should have metadata
            expect(response.metadata).not_to_be_nil("No metadata received")
            expect(response.metadata.request_id).not_to_be_nil("No request ID in metadata")
        end)

        it("should handle reasoning model parameters correctly", function()
            -- Skip test if integration tests are disabled
            if not RUN_INTEGRATION_TESTS then
                print("Skipping integration test - not enabled")
                return
            end

            -- Reasoning models might not be available in all environments
            print("Warning: This test may fail if Claude or GPT-4 with reasoning is not available")

            -- Restore original functions for integration test
            restore_all_mocks()

            -- Make a real API call with reasoning parameters
            local response = text_generation.handler({
                model = "gpt-4",       -- Use a model that supports reasoning if available
                message = "Solve this step by step: If 2+2=4 and 3+3=6, what is 5+5?",
                thinking_effort = 0.7, -- High reasoning effort
                temperature = 0        -- Deterministic
            })

            -- Verify response
            expect(response.error).to_be_nil("API request failed: " ..
                (response.error_message or "unknown error"))
            expect(response.result).not_to_be_nil("No response received from API")

            -- Should have token usage
            expect(response.tokens).not_to_be_nil("No token usage information received")
            expect(response.tokens.prompt_tokens > 0).to_be_true("No prompt tokens reported")
            expect(response.tokens.completion_tokens > 0).to_be_true("No completion tokens reported")

            -- Note: thinking_tokens may not be present if the model doesn't support it
            -- or if the API didn't return this information
        end)

        it("should handle API errors gracefully in integration test", function()
            -- Skip test if integration tests are disabled
            if not RUN_INTEGRATION_TESTS then
                print("Skipping integration test - not enabled")
                return
            end

            -- Restore original functions for integration test
            restore_all_mocks()

            -- Make a real API call with invalid model
            local response = text_generation.handler({
                model = "nonexistent-model-name",
                message = "This should fail"
            })

            -- Should have error information
            expect(response.error).not_to_be_nil("Expected error but none received")
            expect(response.error_message).not_to_be_nil("No error message received")

            -- Most likely an invalid model error
            expect(response.error_message:find("model")).not_to_be_nil(
                "Expected model-related error message but got: " .. response.error_message
            )
        end)
    end)
end

return require("test").run_cases(define_tests)
