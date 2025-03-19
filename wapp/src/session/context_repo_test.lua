local sql = require("sql")
local test = require("test")
local uuid = require("uuid")
local context_repo = require("context_repo")

local function define_tests()
    describe("Context Repository", function()
        -- Test data
        local test_data = {
            context_id = uuid.v7(),
            context_id2 = uuid.v7()
        }

        -- Clean up test data after all tests
        after_all(function()
            -- Get a database connection for cleanup
            local db, err = sql.get("app:db")
            if err then
                error("Failed to connect to database: " .. err)
            end

            -- Delete test contexts
            db:execute("DELETE FROM contexts WHERE context_id IN (?, ?)",
                      { test_data.context_id, test_data.context_id2 })

            db:release()
        end)

        it("should create a new context", function()
            local context, err = context_repo.create(
                test_data.context_id,
                "test_type",
                "This is test context data"
            )

            expect(err).to_be_nil()
            expect(context).not_to_be_nil()
            expect(context.context_id).to_equal(test_data.context_id)
            expect(context.type).to_equal("test_type")
        end)

        it("should get a context by ID", function()
            local context, err = context_repo.get(test_data.context_id)

            expect(err).to_be_nil()
            expect(context).not_to_be_nil()
            expect(context.context_id).to_equal(test_data.context_id)
            expect(context.type).to_equal("test_type")
            expect(context.data).to_equal("This is test context data")
        end)

        it("should create another context of same type", function()
            local context, err = context_repo.create(
                test_data.context_id2,
                "test_type",
                "Another test context data"
            )

            expect(err).to_be_nil()
            expect(context).not_to_be_nil()
            expect(context.context_id).to_equal(test_data.context_id2)
        end)

        it("should get contexts by type", function()
            local contexts, err = context_repo.get_by_type("test_type")

            expect(err).to_be_nil()
            expect(contexts).not_to_be_nil()
            expect(#contexts >= 2).to_be_true()

            -- Find our test contexts in the results
            local found_id1 = false
            local found_id2 = false

            for _, context in ipairs(contexts) do
                if context.context_id == test_data.context_id then
                    found_id1 = true
                end
                if context.context_id == test_data.context_id2 then
                    found_id2 = true
                end
            end

            expect(found_id1).to_be_true()
            expect(found_id2).to_be_true()
        end)

        it("should get contexts by type with limit and offset", function()
            -- First, get with limit 1
            local contexts, err = context_repo.get_by_type("test_type", 1)

            expect(err).to_be_nil()
            expect(contexts).not_to_be_nil()
            expect(#contexts).to_equal(1)

            -- Then, get with offset 1 to get the second record
            contexts, err = context_repo.get_by_type("test_type", 1, 1)

            expect(err).to_be_nil()
            expect(contexts).not_to_be_nil()
            expect(#contexts).to_equal(1)

            -- The two contexts should be different
            local first_context_id = nil
            local second_context_id = nil

            -- Get first context
            contexts, err = context_repo.get_by_type("test_type", 1)
            first_context_id = contexts[1].context_id

            -- Get second context
            contexts, err = context_repo.get_by_type("test_type", 1, 1)
            second_context_id = contexts[1].context_id

            expect(first_context_id).not_to_equal(second_context_id)
        end)

        it("should update context data", function()
            local updated, err = context_repo.update(
                test_data.context_id,
                "Updated context data"
            )

            expect(err).to_be_nil()
            expect(updated).not_to_be_nil()
            expect(updated.context_id).to_equal(test_data.context_id)
            expect(updated.updated).to_be_true()

            -- Verify the update by getting the context
            local context, err = context_repo.get(test_data.context_id)
            expect(context.data).to_equal("Updated context data")
        end)

        it("should delete a context", function()
            local result, err = context_repo.delete(test_data.context_id2)

            expect(err).to_be_nil()
            expect(result).not_to_be_nil()
            expect(result.deleted).to_be_true()

            -- Verify the deletion by trying to get the context
            local context, err = context_repo.get(test_data.context_id2)
            expect(context).to_be_nil()
            expect(err).not_to_be_nil()
            expect(err:match("not found")).not_to_be_nil()
        end)

        it("should handle validation errors", function()
            -- Missing context_id
            local context, err = context_repo.create(nil, "test_type", "data")
            expect(context).to_be_nil()
            expect(err:match("Context ID is required")).not_to_be_nil()

            -- Missing type
            context, err = context_repo.create(uuid.v7(), "", "data")
            expect(context).to_be_nil()
            expect(err:match("Context type is required")).not_to_be_nil()

            -- Get with invalid ID
            context, err = context_repo.get("")
            expect(context).to_be_nil()
            expect(err:match("Context ID is required")).not_to_be_nil()

            -- Update with invalid ID
            local result, err = context_repo.update("", "data")
            expect(result).to_be_nil()
            expect(err:match("Context ID is required")).not_to_be_nil()

            -- Update non-existent context
            result, err = context_repo.update(uuid.v7(), "data")
            expect(result).to_be_nil()
            expect(err:match("Context not found")).not_to_be_nil()

            -- Delete with invalid ID
            result, err = context_repo.delete("")
            expect(result).to_be_nil()
            expect(err:match("Context ID is required")).not_to_be_nil()

            -- Delete non-existent context
            result, err = context_repo.delete(uuid.v7())
            expect(result).to_be_nil()
            expect(err:match("Context not found")).not_to_be_nil()
        end)
    end)
end

return test.run_cases(define_tests)