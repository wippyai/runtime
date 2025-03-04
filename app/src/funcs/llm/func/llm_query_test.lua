local http = require("http_client")
local json = require("json")
local env = require("env")
local llm_query = require("llm_query")

-- Define tests
local function define_tests()
    describe("LLM Query Module", function()
        -- Check if we should run integration tests
        local run_integration_tests = false
        local actual_api_key = nil

        before_all(function()
            -- Check if we have a real API key for integration tests
            actual_api_key = env.get("OPENAI_KEY")
            run_integration_tests = actual_api_key and #actual_api_key > 10

            if run_integration_tests then
                print("Integration tests will run with real API key")
            else
                print("Skipping integration tests - no valid API key found")
            end
        end)

        -- Setup mocks before each test
        before_each(function()
            -- Mock env.get to return a fake API key for unit tests
            mock(env, "get", function(key)
                if key == "OPENAI_KEY" then
                    return "test-api-key-12345"
                end
                return actual_api_key
            end)
        end)

        -- Automatically restored after each test by the framework

        it("should handle non-streaming LLM query", function()
            -- Mock HTTP response for non-streaming
            mock(http, "post", function(endpoint, options)
                -- Verify request
                expect(endpoint).to_equal("https://api.openai.com/v1/chat/completions")
                expect(options.headers["Authorization"]).to_equal("Bearer test-api-key-12345")
                expect(options.headers["Content-Type"]).to_equal("application/json")

                -- Parse request body
                local request_body = json.decode(options.body)
                expect(request_body.model).to_equal("gpt-4o")
                expect(request_body.stream).to_be_false()
                expect(#request_body.messages).to_equal(1)
                expect(request_body.messages[1].role).to_equal("user")
                expect(request_body.messages[1].content).to_equal("Test message")

                -- Return mock response
                return {
                    status_code = 200,
                    body = json.encode({
                        choices = {
                            {
                                message = {
                                    content = "This is a test response"
                                }
                            }
                        }
                    })
                }
            end)

            -- Call the handler function
            local response, err = llm_query.handler({
                message = "Test message",
                endpoint = "https://api.openai.com/v1/chat/completions"
            })

            -- Verify response
            expect(err).to_be_nil()
            expect(response).to_equal("This is a test response")
        end)

        it("should handle history in LLM query", function()
            -- Mock HTTP response
            mock(http, "post", function(endpoint, options)
                -- Parse request body to check messages
                local request_body = json.decode(options.body)
                expect(#request_body.messages).to_equal(3)
                expect(request_body.messages[1].role).to_equal("user")
                expect(request_body.messages[1].content).to_equal("First message")
                expect(request_body.messages[2].role).to_equal("assistant")
                expect(request_body.messages[2].content).to_equal("First response")
                expect(request_body.messages[3].role).to_equal("user")
                expect(request_body.messages[3].content).to_equal("Second message")

                -- Return mock response
                return {
                    status_code = 200,
                    body = json.encode({
                        choices = {
                            {
                                message = {
                                    content = "Second response"
                                }
                            }
                        }
                    })
                }
            end)

            -- Call the handler function with history
            local response = llm_query.handler({
                message = "Second message",
                history = {
                    { role = "user", content = "First message" },
                    { role = "assistant", content = "First response" }
                }
            })

            -- Verify response
            expect(response).to_equal("Second response")
        end)

        it_skip("should handle streaming LLM query", function()
            -- Set up process.send mock to capture streamed responses
            local received_messages = {}

            -- Set up process.send mock to capture streamed responses
            local received_messages = {}
            mock("process.send", function(target, type, data)
                table.insert(received_messages, { target = target, type = type, data = data })
            end)

            -- Create a mock stream reader
            local mock_stream = {
                chunks = {
                    'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
                    'data: {"choices":[{"delta":{"content":", "}}]}\n\n',
                    'data: {"choices":[{"delta":{"content":"world"}}]}\n\n',
                    'data: {"choices":[{"delta":{"content":"!"}}]}\n\n',
                    'data: [DONE]\n\n'
                },
                chunk_index = 0,
                read = function(self)
                    self.chunk_index = self.chunk_index + 1
                    return self.chunk_index <= #self.chunks and self.chunks[self.chunk_index] or nil
                end,
                close = function() end
            }

            -- Mock HTTP response for streaming
            mock(http, "post", function(endpoint, options)
                -- Verify request
                expect(options.stream).not_to_be_nil()
                expect(options.stream.buffer_size).to_equal(4096)

                -- Parse request body
                local request_body = json.decode(options.body)
                expect(request_body.stream).to_be_true()

                -- Return mock streaming response
                return {
                    status_code = 200,
                    stream = mock_stream
                }
            end)

            -- Call the handler function in streaming mode
            local response = llm_query.handler({
                message = "Test streaming",
                stream = true,
                reply_to = "test-pid-12345"
            })

            print(response)

            -- Verify streamed responses
            expect(#received_messages).to_be_type("number")
            expect(#received_messages >= 3).to_be_true()

            -- Check that all messages have correct target and type
            for _, msg in ipairs(received_messages) do
                expect(msg.target).to_equal("test-pid-12345")
                expect(msg.type).to_equal("response")
            end

            -- Verify that at least one non-final message exists with text content
            local text_found = false
            for i = 1, #received_messages - 1 do
                if received_messages[i].data.text then
                    text_found = true
                    expect(received_messages[i].data.done).to_be_false()
                end
            end
            expect(text_found).to_be_true("No text content found in streamed messages")

            -- Last message should indicate completion
            local last_msg = received_messages[#received_messages]
            expect(last_msg.data.done).to_be_true()

            -- Full response should be concatenated and returned
            expect(response).to_equal("Hello, world!")
        end)

        it("should handle API errors gracefully", function()
            -- Mock HTTP error response
            mock(http, "post", function()
                return {
                    status_code = 401,
                    body = json.encode({
                        error = {
                            message = "Invalid API key"
                        }
                    })
                }
            end)

            -- Call the handler function
            local response, err = llm_query.handler({
                message = "Test error handling"
            })

            -- Verify error handling
            expect(response).to_be_nil()
            expect(err).not_to_be_nil()
            expect(err).to_equal("Failed to get LLM response")
        end)

        it("should handle custom model parameter", function()
            -- Mock HTTP response
            mock(http, "post", function(endpoint, options)
                -- Parse request body
                local request_body = json.decode(options.body)
                expect(request_body.model).to_equal("custom-model-name")

                -- Return mock response
                return {
                    status_code = 200,
                    body = json.encode({
                        choices = {
                            {
                                message = {
                                    content = "Custom model response"
                                }
                            }
                        }
                    })
                }
            end)

            -- Call the handler function with custom model
            local response = llm_query.handler({
                message = "Test custom model",
                model = "custom-model-name"
            })

            -- Verify response
            expect(response).to_equal("Custom model response")
        end)
    end)

    -- Integration tests with real API
    describe("LLM Query Integration", function()
        it_skip("should connect to OpenAI API and get a response", function()
            -- Check if we have a real API key to use
            local actual_api_key = env.get("OPENAI_KEY")
            if not actual_api_key or #actual_api_key < 10 then
                print("Skipping integration test - no valid API key")
                return
            end

            -- Restore original functions for integration test
            restore_all_mocks()

            -- Make a real API call
            local response, err = llm_query.handler({
                message = "Reply with exactly 'Integration test successful'",
                endpoint = "https://api.openai.com/v1/chat/completions",
                model = "gpt-4o-mini" -- Faster and cheaper model
            })

            -- Verify response
            expect(err).to_be_nil("API request failed: " .. (err or "unknown error"))
            expect(response).not_to_be_nil("No response received from API")
            --expect(response:find("Integration test successful")).not_to_be_nil(
            --    "Expected phrase not found in response: " .. response
            --)
        end)

        it_skip("should handle streaming with real API", function()
            -- Check if we have a real API key to use
            local actual_api_key = env.get("OPENAI_KEY")
            if not actual_api_key or #actual_api_key < 10 then
                print("Skipping streaming integration test - no valid API key")
                return
            end

            -- Restore original functions for integration test
            restore_all_mocks()

            -- Set up to capture streamed responses
            local received_chunks = {}
            mock("process.send", function(target, type, data)
                if type == "response" and not data.done then
                    table.insert(received_chunks, data.text or "")
                end
            end)

            -- Make a real streaming API call
            local response = llm_query.handler({
                message = "Count from 1 to 5 slowly",
                endpoint = "https://api.openai.com/v1/chat/completions",
                model = "gpt-4o-mini", -- Faster and cheaper model
                stream = true,
                reply_to = "test-integration-pid"
            })

            -- Verify streaming worked
            expect(#received_chunks).to_be_type("number")
            expect(#received_chunks > 0).to_be_true("No chunks received from streaming API")
            expect(response).not_to_be_nil("No full response received")
            expect(#response > 0).to_be_true("Empty full response received")
        end)
    end)
end

return require("test").run_cases(define_tests)