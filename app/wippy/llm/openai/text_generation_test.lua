local text_generation = require("text_generation")
local openai_client = require("openai_client")
local output = require("output")
local json = require("json")
local env = require("env")

local function define_tests()
    -- Toggle to enable/disable real API integration test
    local RUN_INTEGRATION_TESTS = env.get("ENABLE_INTEGRATION_TESTS")

    describe("OpenAI Text Generation Handler", function()
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
                print("Integration tests disabled - set ENABLE_INTEGRATION_TESTS=true to enable")
            end
        end)

        it("should successfully generate text with mocked client", function()
            -- Mock the client request function
            mock(openai_client, "request", function(endpoint_path, payload, options)
                -- Validate the request
                expect(endpoint_path).to_equal(openai_client.DEFAULT_CHAT_ENDPOINT)
                expect(payload.model).to_equal("gpt-4")
                expect(payload.messages[1].role).to_equal("user")
                expect(payload.messages[1].content).to_equal("Say hello world")

                -- Return mock successful response
                return {
                    choices = {
                        {
                            message = {
                                content = "Hello, world!"
                            },
                            finish_reason = "stop"
                        }
                    },
                    usage = {
                        prompt_tokens = 10,
                        completion_tokens = 5,
                        total_tokens = 15
                    },
                    metadata = {
                        request_id = "req_mocktest123",
                        processing_ms = 150
                    }
                }
            end)

            -- Call with a simple message
            local response = text_generation.handler({
                model = "gpt-4",
                message = "Say hello world"
            })

            -- Verify the response structure
            expect(response.error).to_be_nil("Expected no error")
            expect(response.result).to_equal("Hello, world!")
            expect(response.tokens).not_to_be_nil("Expected token information")
            expect(response.tokens.prompt_tokens).to_equal(10)
            expect(response.tokens.completion_tokens).to_equal(5)
            expect(response.tokens.total_tokens).to_equal(15)
            expect(response.metadata).not_to_be_nil("Expected metadata")
            expect(response.metadata.request_id).to_equal("req_mocktest123")
            expect(response.finish_reason).to_equal("stop")
        end)

        it("should connect to real OpenAI API with gpt-4o-mini model", function()
            -- Skip test if integration tests are disabled
            if not RUN_INTEGRATION_TESTS then
                print("Skipping integration test - not enabled")
                return
            end

            -- Restore original functions for integration test
            restore_all_mocks()

            -- Make a real API call with gpt-4o-mini
            local response = text_generation.handler({
                model = "gpt-4o-mini",
                message = "Reply with exactly the text 'Integration test successful'",
                temperature = 0, -- Deterministic output
                max_tokens = 15  -- Short response
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
    end)
end

return require("test").run_cases(define_tests)
