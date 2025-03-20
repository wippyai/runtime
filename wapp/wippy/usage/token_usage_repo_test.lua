local sql = require("sql")
local test = require("test")
local uuid = require("uuid")
local token_usage_repo = require("token_usage_repo")

local function define_tests()
    describe("Token Usage Repository", function()
        -- Test data
        local test_data = {
            user_id = uuid.v7(),
            context_id = uuid.v7()
        }

        -- Setup test environment before all tests
        before_all(function()
            -- No setup needed, we'll just use the context_id directly
            -- since we're removing the dependency on context_repo
        end)

        -- Clean up test data after all tests
        after_all(function()
            -- Get a database connection for cleanup
            local db, err = sql.get("app:db")
            if err then
                error("Failed to connect to database: " .. err)
            end

            -- Begin transaction for cleanup
            local tx, err = db:begin()
            if err then
                db:release()
                error("Failed to begin transaction: " .. err)
            end

            -- Delete test data
            tx:execute("DELETE FROM token_usage WHERE user_id = ?", { test_data.user_id })

            -- Commit transaction
            local success, err = tx:commit()
            if err then
                tx:rollback()
                db:release()
                error("Failed to commit cleanup transaction: " .. err)
            end

            db:release()
        end)

        it("should record token usage", function()
            local usage, err = token_usage_repo.record(
                test_data.user_id,
                test_data.context_id,
                "test-model",
                100, -- prompt_tokens
                50,  -- completion_tokens
                25   -- thinking_tokens
            )

            expect(err).to_be_nil()
            expect(usage).not_to_be_nil()
            expect(usage.usage_id).not_to_be_nil()
            expect(usage.user_id).to_equal(test_data.user_id)
            expect(usage.context_id).to_equal(test_data.context_id)
            expect(usage.model_name).to_equal("test-model")
            expect(usage.prompt_tokens).to_equal(100)
            expect(usage.completion_tokens).to_equal(50)
            expect(usage.thinking_tokens).to_equal(25)
            expect(usage.total_tokens).to_equal(175) -- Sum of all token types
            expect(usage.timestamp).not_to_be_nil()

            -- Save the usage ID for later tests
            test_data.usage_id = usage.usage_id
        end)

        it("should get token usage by ID", function()
            local usage, err = token_usage_repo.get(test_data.usage_id)

            expect(err).to_be_nil()
            expect(usage).not_to_be_nil()
            expect(usage.usage_id).to_equal(test_data.usage_id)
            expect(usage.user_id).to_equal(test_data.user_id)
            expect(usage.context_id).to_equal(test_data.context_id)
            expect(usage.model_name).to_equal("test-model")
            expect(usage.prompt_tokens).to_equal(100)
            expect(usage.completion_tokens).to_equal(50)
            expect(usage.thinking_tokens).to_equal(25)
            expect(usage.total_tokens).to_equal(175)
        end)

        it("should get token usage by user", function()
            local usages, err = token_usage_repo.get_by_user(test_data.user_id)

            expect(err).to_be_nil()
            expect(usages).not_to_be_nil()
            expect(#usages).to_equal(1)
            expect(usages[1].usage_id).to_equal(test_data.usage_id)
        end)

        it("should get token usage by context", function()
            local usages, err = token_usage_repo.get_by_context(test_data.context_id)

            expect(err).to_be_nil()
            expect(usages).not_to_be_nil()
            expect(#usages).to_equal(1)
            expect(usages[1].usage_id).to_equal(test_data.usage_id)
        end)

        it("should get context token totals", function()
            local totals, err = token_usage_repo.get_context_totals(test_data.context_id)

            expect(err).to_be_nil()
            expect(totals).not_to_be_nil()
            expect(totals.context_id).to_equal(test_data.context_id)
            expect(totals.prompt_tokens).to_equal(100)
            expect(totals.completion_tokens).to_equal(50)
            expect(totals.thinking_tokens).to_equal(25)
            expect(totals.total_tokens).to_equal(175)
            expect(totals.request_count).to_equal(1)
        end)

        it("should get token usage by model", function()
            local usage, err = token_usage_repo.get_by_model("test-model")

            expect(err).to_be_nil()
            expect(usage).not_to_be_nil()
            expect(usage.model_name).to_equal("test-model")
            expect(usage.prompt_tokens).to_equal(100)
            expect(usage.completion_tokens).to_equal(50)
            expect(usage.thinking_tokens).to_equal(25)
            expect(usage.total_tokens).to_equal(175)
            expect(usage.request_count).to_equal(1)
        end)

        it("should get user token totals", function()
            local totals, err = token_usage_repo.get_user_totals(test_data.user_id)

            expect(err).to_be_nil()
            expect(totals).not_to_be_nil()
            expect(totals.user_id).to_equal(test_data.user_id)
            expect(totals.prompt_tokens).to_equal(100)
            expect(totals.completion_tokens).to_equal(50)
            expect(totals.thinking_tokens).to_equal(25)
            expect(totals.total_tokens).to_equal(175)
            expect(totals.request_count).to_equal(1)
            expect(totals.context_count).to_equal(1)
        end)

        it("should get daily token usage", function()
            local daily, err = token_usage_repo.get_daily_usage()

            expect(err).to_be_nil()
            expect(daily).not_to_be_nil()
            expect(#daily >= 1).to_be_true()

            -- Find our test data in the results (there might be other data in the DB)
            local found = false
            for _, day in ipairs(daily) do
                if day.prompt_tokens >= 100 and
                    day.completion_tokens >= 50 and
                    day.thinking_tokens >= 25 then
                    found = true
                    break
                end
            end

            expect(found).to_be_true()
        end)

        it("should handle missing records gracefully", function()
            local missing_id = uuid.v7()

            local usage, err = token_usage_repo.get(missing_id)
            expect(err).not_to_be_nil()
            expect(usage).to_be_nil()
            expect(err:match("not found")).not_to_be_nil()

            local context_totals = token_usage_repo.get_context_totals(missing_id)
            expect(context_totals.total_tokens).to_equal(0)

            local model_totals = token_usage_repo.get_by_model("nonexistent-model")
            expect(model_totals.total_tokens).to_equal(0)
        end)

        it("should record token usage without a context", function()
            local usage, err = token_usage_repo.record(
                test_data.user_id,
                nil, -- No context_id
                "test-model",
                100, -- prompt_tokens
                50,  -- completion_tokens
                25   -- thinking_tokens
            )

            expect(err).to_be_nil()
            expect(usage).not_to_be_nil()
            expect(usage.usage_id).not_to_be_nil()
            expect(usage.user_id).to_equal(test_data.user_id)
            expect(usage.context_id).to_be_nil()
            expect(usage.model_name).to_equal("test-model")
            expect(usage.total_tokens).to_equal(175)

            -- No need to clean up this record as we'll delete all token usage
            -- for this user ID in the after_all hook
        end)
    end)
end

return test.run_cases(define_tests)