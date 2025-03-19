local sql = require("sql")
local test = require("test")
local uuid = require("uuid")
local json = require("json")
local message_repo = require("message_repo")
local session_repo = require("session_repo")
local context_repo = require("context_repo")

local function define_tests()
    describe("Message Repository", function()
        -- Test data
        local test_data = {
            user_id = uuid.v7(),
            context_id = uuid.v7(),
            session_id = uuid.v7(),
            message_id = uuid.v7(),
            message_id2 = uuid.v7()
        }

        -- Setup test environment before all tests
        before_all(function()
            -- Create a test context
            local context, err = context_repo.create(
                test_data.context_id,
                "primary",
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

        it("should create a message with string data", function()
            local message, err = message_repo.create(
                test_data.message_id,
                test_data.session_id,
                "user",
                "This is a test message"
            )

            expect(err).to_be_nil()
            expect(message).not_to_be_nil()
            expect(message.message_id).to_equal(test_data.message_id)
            expect(message.session_id).to_equal(test_data.session_id)
            expect(message.type).to_equal("user")
            expect(message.date).not_to_be_nil()
        end)

        it("should create a message with binary data and metadata", function()
            local metadata = {
                model = "test-model",
                tokens = {
                    prompt = 10,
                    completion = 5
                }
            }

            local message, err = message_repo.create(
                test_data.message_id2,
                test_data.session_id,
                "assistant",
                "This is a response message",
                metadata
            )

            expect(err).to_be_nil()
            expect(message).not_to_be_nil()
            expect(message.message_id).to_equal(test_data.message_id2)
            expect(message.session_id).to_equal(test_data.session_id)
            expect(message.type).to_equal("assistant")
        end)

        it("should get a message by ID", function()
            local message, err = message_repo.get(test_data.message_id)

            expect(err).to_be_nil()
            expect(message).not_to_be_nil()
            expect(message.message_id).to_equal(test_data.message_id)
            expect(message.session_id).to_equal(test_data.session_id)
            expect(message.type).to_equal("user")
            expect(message.data).to_equal("This is a test message")
        end)

        it("should parse metadata JSON when retrieving", function()
            local message, err = message_repo.get(test_data.message_id2)

            expect(err).to_be_nil()
            expect(message).not_to_be_nil()
            expect(message.metadata).not_to_be_nil()
            expect(message.metadata.model).to_equal("test-model")
            expect(message.metadata.tokens.prompt).to_equal(10)
            expect(message.metadata.tokens.completion).to_equal(5)
        end)

        it("should list messages by session ID", function()
            local messages, err = message_repo.list_by_session(test_data.session_id)

            expect(err).to_be_nil()
            expect(messages).not_to_be_nil()
            expect(#messages).to_equal(2)
        end)

        it("should list messages by type", function()
            local messages, err = message_repo.list_by_type(test_data.session_id, "user")

            expect(err).to_be_nil()
            expect(messages).not_to_be_nil()
            expect(#messages).to_equal(1)
            expect(messages[1].type).to_equal("user")

            messages, err = message_repo.list_by_type(test_data.session_id, "assistant")
            expect(err).to_be_nil()
            expect(messages).not_to_be_nil()
            expect(#messages).to_equal(1)
            expect(messages[1].type).to_equal("assistant")
        end)

        it("should get the latest message", function()
            local message, err = message_repo.get_latest(test_data.session_id)

            expect(err).to_be_nil()
            expect(message).not_to_be_nil()
            -- The most recent message should be the assistant message (the second one created)
            expect(message.message_id).to_equal(test_data.message_id2)
            expect(message.type).to_equal("assistant")
        end)

        it("should count messages in a session", function()
            local count, err = message_repo.count_by_session(test_data.session_id)

            expect(err).to_be_nil()
            expect(count).to_equal(2)
        end)

        it("should count messages by type", function()
            local count, err = message_repo.count_by_type(test_data.session_id, "user")

            expect(err).to_be_nil()
            expect(count).to_equal(1)

            count, err = message_repo.count_by_type(test_data.session_id, "assistant")
            expect(err).to_be_nil()
            expect(count).to_equal(1)

            count, err = message_repo.count_by_type(test_data.session_id, "system")
            expect(err).to_be_nil()
            expect(count).to_equal(0)
        end)

        it("should delete a message", function()
            -- First verify we can get the message
            local message, err = message_repo.get(test_data.message_id)
            expect(err).to_be_nil()
            expect(message).not_to_be_nil()

            -- Now delete it
            local result, err = message_repo.delete(test_data.message_id)

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.deleted).to_be_true()

            -- Verify the deletion
            message, err = message_repo.get(test_data.message_id)
            expect(message).to_be_nil()
            expect(err:match("not found")).not_to_be_nil()

            -- Count should now be 1
            local count, err = message_repo.count_by_session(test_data.session_id)
            expect(err).to_be_nil()
            expect(count).to_equal(1)
        end)

        it("should handle validation errors", function()
            -- Missing message_id
            local message, err = message_repo.create(nil, test_data.session_id, "user", "data")
            expect(message).to_be_nil()
            expect(err:match("Message ID is required")).not_to_be_nil()

            -- Missing session_id
            message, err = message_repo.create(uuid.v7(), "", "user", "data")
            expect(message).to_be_nil()
            expect(err:match("Session ID is required")).not_to_be_nil()

            -- Missing type
            message, err = message_repo.create(uuid.v7(), test_data.session_id, "", "data")
            expect(message).to_be_nil()
            expect(err:match("Message type is required")).not_to_be_nil()

            -- Missing data
            message, err = message_repo.create(uuid.v7(), test_data.session_id, "user", nil)
            expect(message).to_be_nil()
            expect(err:match("Message data is required")).not_to_be_nil()

            -- Non-existent session
            message, err = message_repo.create(uuid.v7(), uuid.v7(), "user", "data")
            expect(message).to_be_nil()
            expect(err:match("Session not found")).not_to_be_nil()

            -- Get with invalid ID
            message, err = message_repo.get("")
            expect(message).to_be_nil()
            expect(err:match("Message ID is required")).not_to_be_nil()

            -- List by invalid session ID
            local messages, err = message_repo.list_by_session("")
            expect(messages).to_be_nil()
            expect(err:match("Session ID is required")).not_to_be_nil()

            -- Delete with invalid ID
            local result, err = message_repo.delete("")
            expect(result).to_be_nil()
            expect(err:match("Message ID is required")).not_to_be_nil()
        end)
    end)
end

return test.run_cases(define_tests)