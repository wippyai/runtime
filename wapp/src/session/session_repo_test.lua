local sql = require("sql")
local test = require("test")
local uuid = require("uuid")
local session_repo = require("session_repo")
local context_repo = require("context_repo")

local function define_tests()
    describe("Session Repository", function()
        -- Test data
        local test_data = {
            user_id = uuid.v7(),
            context_id = uuid.v7(),
            context_id2 = uuid.v7(),
            session_id = uuid.v7()
        }

        -- Setup test context before all tests
        before_all(function()
            -- Create primary context for sessions
            local context, err = context_repo.create(
                test_data.context_id,
                "primary",
                "Primary context data"
            )

            if err then
                error("Failed to create primary test context: " .. err)
            end

            -- Create secondary context for testing relationships
            context, err = context_repo.create(
                test_data.context_id2,
                "secondary",
                "Secondary context data"
            )

            if err then
                error("Failed to create secondary test context: " .. err)
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
            tx:execute("DELETE FROM contexts WHERE context_id IN (?, ?)",
                { test_data.context_id, test_data.context_id2 })

            -- Commit transaction
            local success, err = tx:commit()
            if err then
                tx:rollback()
                db:release()
                error("Failed to commit cleanup transaction: " .. err)
            end

            db:release()
        end)

        it("should create a new session", function()
            local session, err = session_repo.create(
                test_data.session_id,
                test_data.user_id,
                test_data.context_id,
                "Test Session",
                "test",
                "test-model",
                "test-agent"
            )

            expect(err).to_be_nil()
            expect(session).not_to_be_nil()
            expect(session.session_id).to_equal(test_data.session_id)
            expect(session.user_id).to_equal(test_data.user_id)
            expect(session.primary_context_id).to_equal(test_data.context_id)
            expect(session.title).to_equal("Test Session")
            expect(session.kind).to_equal("test")
            expect(session.current_model).to_equal("test-model")
            expect(session.current_agent).to_equal("test-agent")
            expect(session.start_date).not_to_be_nil()
            expect(session.last_message_date).not_to_be_nil()
        end)

        it("should get a session by ID", function()
            local session, err = session_repo.get(test_data.session_id)

            expect(err).to_be_nil()
            expect(session).not_to_be_nil()
            expect(session.session_id).to_equal(test_data.session_id)
            expect(session.user_id).to_equal(test_data.user_id)
            expect(session.primary_context_id).to_equal(test_data.context_id)
            expect(session.title).to_equal("Test Session")
            expect(session.kind).to_equal("test")
        end)

        it("should list sessions by user ID", function()
            local sessions, err = session_repo.list_by_user(test_data.user_id)

            expect(err).to_be_nil()
            expect(sessions).not_to_be_nil()
            expect(#sessions >= 1).to_be_true()

            -- Find our test session
            local found = false
            for _, session in ipairs(sessions) do
                if session.session_id == test_data.session_id then
                    found = true
                    break
                end
            end

            expect(found).to_be_true()
        end)

        it("should update session title", function()
            local result, err = session_repo.update_title(
                test_data.session_id,
                "Updated Session Title"
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.session_id).to_equal(test_data.session_id)
            expect(result.title).to_equal("Updated Session Title")
            expect(result.updated).to_be_true()

            -- Verify the update
            local session, err = session_repo.get(test_data.session_id)
            expect(session.title).to_equal("Updated Session Title")
        end)

        it("should update last message date", function()
            local now = os.time() - 3600 -- 1 hour ago
            local result, err = session_repo.update_last_message_date(
                test_data.session_id,
                now
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.session_id).to_equal(test_data.session_id)
            expect(result.last_message_date).to_equal(now)
            expect(result.updated).to_be_true()

            -- Verify the update
            local session, err = session_repo.get(test_data.session_id)
            expect(session.last_message_date).to_equal(now)
        end)

        it("should update session metadata", function()
            local updates = {
                title = "Meta Updated Title",
                current_model = "new-model",
                current_agent = "new-agent",
                last_message_date = os.time()
            }

            local result, err = session_repo.update_session_meta(
                test_data.session_id,
                updates
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.session_id).to_equal(test_data.session_id)
            expect(result.title).to_equal(updates.title)
            expect(result.current_model).to_equal(updates.current_model)
            expect(result.current_agent).to_equal(updates.current_agent)
            expect(result.last_message_date).to_equal(updates.last_message_date)
            expect(result.updated).to_be_true()

            -- Verify the update
            local session, err = session_repo.get(test_data.session_id)
            expect(session.title).to_equal(updates.title)
            expect(session.current_model).to_equal(updates.current_model)
            expect(session.current_agent).to_equal(updates.current_agent)
            expect(session.last_message_date).to_equal(updates.last_message_date)
        end)

        it("should add a context to a session", function()
            local result, err = session_repo.add_context(
                test_data.session_id,
                test_data.context_id2
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.session_id).to_equal(test_data.session_id)
            expect(result.context_id).to_equal(test_data.context_id2)
            expect(result.added).to_be_true()

            -- Try adding the same context again
            result, err = session_repo.add_context(
                test_data.session_id,
                test_data.context_id2
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.added).to_be_false()
            expect(result.message).to_match("already exists")
        end)

        it("should get all contexts for a session", function()
            local contexts, err = session_repo.get_contexts(test_data.session_id)

            expect(err).to_be_nil()
            expect(contexts).not_to_be_nil()
            expect(#contexts).to_equal(1) -- Only the secondary context is linked via session_contexts
            expect(contexts[1].context_id).to_equal(test_data.context_id2)
        end)

        it("should remove a context from a session", function()
            local result, err = session_repo.remove_context(
                test_data.session_id,
                test_data.context_id2
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.session_id).to_equal(test_data.session_id)
            expect(result.context_id).to_equal(test_data.context_id2)
            expect(result.removed).to_be_true()

            -- Try removing the same context again
            result, err = session_repo.remove_context(
                test_data.session_id,
                test_data.context_id2
            )

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.removed).to_be_false()
            expect(result.message).to_match("did not exist")

            -- Verify no contexts are linked now
            local contexts, err = session_repo.get_contexts(test_data.session_id)
            expect(#contexts).to_equal(0)
        end)

        it("should handle validation errors", function()
            -- Invalid session creation
            local session, err = session_repo.create(nil, test_data.user_id, test_data.context_id)
            expect(session).to_be_nil()
            expect(err:match("Session ID is required")).not_to_be_nil()

            session, err = session_repo.create(uuid.v7(), "", test_data.context_id)
            expect(session).to_be_nil()
            expect(err:match("User ID is required")).not_to_be_nil()

            session, err = session_repo.create(uuid.v7(), test_data.user_id, "")
            expect(session).to_be_nil()
            expect(err:match("Primary context ID is required")).not_to_be_nil()

            -- Get with invalid ID
            session, err = session_repo.get("")
            expect(session).to_be_nil()
            expect(err:match("Session ID is required")).not_to_be_nil()

            -- List with invalid user ID
            local sessions, err = session_repo.list_by_user("")
            expect(sessions).to_be_nil()
            expect(err:match("User ID is required")).not_to_be_nil()

            -- Update title with invalid session ID
            local result, err = session_repo.update_title("", "title")
            expect(result).to_be_nil()
            expect(err:match("Session ID is required")).not_to_be_nil()

            -- Update non-existent session
            result, err = session_repo.update_title(uuid.v7(), "title")
            expect(result).to_be_nil()
            expect(err:match("Session not found")).not_to_be_nil()
        end)

        it("should delete a session", function()
            -- First create a session that we can delete
            local temp_session_id = uuid.v7()
            local session, err = session_repo.create(
                temp_session_id,
                test_data.user_id,
                test_data.context_id,
                "Temporary Session"
            )

            expect(err).to_be_nil()

            -- Now delete it
            local result, err = session_repo.delete(temp_session_id)

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.deleted).to_be_true()

            -- Verify the deletion
            session, err = session_repo.get(temp_session_id)
            expect(session).to_be_nil()
            expect(err:match("not found")).not_to_be_nil()

            -- Try to delete a non-existent session
            result, err = session_repo.delete(uuid.v7())
            expect(result).to_be_nil()
            expect(err:match("Session not found")).not_to_be_nil()
        end)
    end)
end

return test.run_cases(define_tests)
