local sql = require("sql")
local test = require("test")
local uuid = require("uuid")
local token_usage_repo = require("token_usage_repo")
local session_repo = require("session_repo")
local context_repo = require("context_repo")

local function define_tests()
    describe("Token Usage Repository", function()
        -- Test data
        local test_data = {
            user_id = uuid.v7(),
            context_id = uuid.v7(),
            session_id = uuid.v7(),
            usage_id = uuid.v7()
        }

        -- Setup test environment before all tests
        before_all(function()
            -- Create a test context
            local context, err = context_repo.create(
                test_data.context_id,
                "test",
                "Test context data"
            )

            if err then
                error("Failed to create test context: " .. err)
            end

            -- Create a test session
            local session, err = session_repo.create(
                test_data.session_id,
                test_data.user_id,
                test_data.context_id,
                "Test Session",
                "test",
                "test-model",
                "test-agent"
            )

            if err then
                error("Failed to create test session: " .. err)
            end
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

            -- Delete test data in proper order (respecting foreign key constraints)
            tx:execute("DELETE FROM token_usage WHERE session_id = ?", { test_data.session_id })
            tx:execute("DELETE FROM messages WHERE session_id = ?", { test_data.session_id })
            tx:execute("DELETE FROM session_contexts WHERE session_id = ?", { test_data.session_id })
            tx:execute("DELETE FROM sessions WHERE session_id = ?", { test_data.session_id })
            tx:execute("DELETE FROM contexts WHERE context_id = ?", { test_data.context_id })

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
                test_data.usage_id,
                test_data.session_id,
                "test-model",
                100, -- prompt_tokens
                50,  -- completion_tokens
                25   -- thinking_tokens
            )

            expect(err).to_be_nil()
            expect(usage).not_to_be_nil()
            expect(usage.usage_id).to_equal(test_data.usage_id)
            expect(usage.session_id).to_equal(test_data.session_id)
            expect(usage.model_name).to_equal("test-model")
            expect(usage.prompt_tokens).to_equal(100)
            expect(usage.completion_tokens).to_equal(50)
            expect(usage.thinking_tokens).to_equal(25)
            expect(usage.total_tokens).to_equal(175) -- Sum of all token types
            expect(usage.timestamp).not_to_be_nil()
        end)

        it("should get token usage by ID", function()
            local usage, err = token_usage_repo.get(test_data.usage_id)

            expect(err).to_be_nil()
            expect(usage).not_to_be_nil()
            expect(usage.usage_id).to_equal(test_data.usage_id)
            expect(usage.session_id).to_equal(test_data.session_id)
            expect(usage.model_name).to_equal("test-model")
            expect(usage.prompt_tokens).to_equal(100)
            expect(usage.completion_tokens).to_equal(50)
            expect(usage.thinking_tokens).to_equal(25)
            expect(usage.total_tokens).to_equal(175)
        end)

        it("should get token usage by session", function()
            local usages, err = token_usage_repo.get_by_session(test_data.session_id)

            expect(err).to_be_nil()
            expect(usages).not_to_be_nil()
            expect(#usages).to_equal(1)
            expect(usages[1].usage_id).to_equal(test_data.usage_id)
        end)

        it("should get session token totals", function()
            local totals, err = token_usage_repo.get_session_totals(test_data.session_id)

            expect(err).to_be_nil()
            expect(totals).not_to_be_nil()
            expect(totals.session_id).to_equal(test_data.session_id)
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
            expect(totals.session_count).to_equal(1)
        end)

        it("should get daily token usage", function()
            local daily, err = token_usage_repo.get_daily_usage()

            expect(err).to_be_nil()
            expect(daily).not_to_be_nil()
            expect(#daily == 1).to_be_true()

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

            local session_totals = token_usage_repo.get_session_totals(missing_id)
            expect(session_totals.total_tokens).to_equal(0)

            local model_totals = token_usage_repo.get_by_model("nonexistent-model")
            expect(model_totals.total_tokens).to_equal(0)
        end)
    end)
end

return test.run_cases(define_tests)
