local actor = require("actor")
local time = require("time")
local json = require("json")

local function define_tests()
    -- Actor Creation and Basic Functionality
    describe("Actor Creation", function()
        it("should create an actor with initial state", function()
            local initial_state = { count = 0 }
            local test_actor = actor.new(initial_state, {})

            expect(test_actor).not_to_be_nil("Actor should be created")
            expect(test_actor.run).to_be_type("function", "Actor should have a run method")
        end)

        it("should throw an error when handlers is not a table", function()
            local success, error_msg = pcall(function()
                actor.new({}, "not a table")
            end)

            expect(success).to_be_false("Should throw an error")
            expect(tostring(error_msg)).to_match("handlers must be a table",
                "Error message should indicate handlers must be a table")
        end)
    end)

    -- Actor.exit Tests
    describe("Actor Exit", function()
        it("should create an exit object with result", function()
            local result = { status = "completed" }
            local exit_obj = actor.exit(result)

            expect(exit_obj).not_to_be_nil("Exit object should be created")
            expect(exit_obj._actor_exit).to_be_true("Exit object should have _actor_exit flag")
            expect(exit_obj.result).to_equal(result, "Exit object should contain the result")
        end)
    end)

    -- Test with dependency injection
    describe("Message Handling with DI", function()
        it("should handle inbox messages", function()
            local handler_called = false

            -- Create a mock message
            local mock_message = {
                topic = function() return "status" end,
                payload = function()
                    return {
                        data = function()
                            return {
                                command = "get_status",
                                reply_to = "caller_pid"
                            }
                        end
                    }
                end
            }

            -- Create mock channels
            local inbox_channel = {
                case_receive = function() return "inbox_case" end
            }

            local events_channel = {
                case_receive = function() return "events_case" end
            }

            -- Create a mock process implementation
            local mock_proc = {
                inbox = function() return inbox_channel end,
                events = function() return events_channel end,
                listen = function(topic)
                    return {
                        case_receive = function() return topic .. "_case" end
                    }
                end,
                send = function(dest, topic, payload)
                    if topic == "status_reply" then
                        expect(dest).to_equal("caller_pid", "Reply should go to correct destination")
                        expect(payload).not_to_be_nil("Reply payload should not be nil")
                    end
                    return true
                end,
                pid = function() return "test-pid" end,
                event = {
                    CANCEL = "pid.cancel",
                    EXIT = "pid.exit",
                    LINK_DOWN = "pid.link.down"
                }
            }

            -- Create channel mock
            local call_count = 0
            mock(_G, "channel", {
                select = function(cases)
                    call_count = call_count + 1

                    -- First call: simulate inbox message
                    if call_count == 1 then
                        return {
                            ok = true,
                            channel = inbox_channel,
                            value = mock_message
                        }
                        -- Second call: exit the loop
                    else
                        return { ok = false }
                    end
                end
            })

            -- Create the actor with dependency injection
            local test_actor = actor.new(
                { value = 42 },
                {
                    status = function(state, msg)
                        handler_called = true
                        expect(msg.command).to_equal("get_status", "Message should have correct command")
                        expect(msg.reply_to).to_equal("caller_pid", "Message should have reply address")
                        return { status = "ok", value = state.value }
                    end
                },
                mock_proc
            )

            -- Run the actor
            test_actor.run()

            -- Verify handler was called
            expect(handler_called).to_be_true("Inbox handler should have been called")
        end)
    end)

    -- Event Handling Tests with DI
    describe("Event Handling with DI", function()
        it("should handle system events with __on_event", function()
            local handler_called = false

            -- Create mock event
            local mock_event = {
                kind = "pid.exit",
                from = "other_pid",
                at = time.now(),
                result = {
                    error = nil,
                    value = { status = "completed" }
                }
            }

            -- Create mock channels
            local inbox_channel = {
                case_receive = function() return "inbox_case" end
            }

            local events_channel = {
                case_receive = function() return "events_case" end
            }

            -- Create a mock process implementation
            local mock_proc = {
                inbox = function() return inbox_channel end,
                events = function() return events_channel end,
                listen = function(topic)
                    return {
                        case_receive = function() return topic .. "_case" end
                    }
                end,
                send = function(dest, topic, payload) return true end,
                pid = function() return "test-pid" end,
                event = {
                    CANCEL = "pid.cancel",
                    EXIT = "pid.exit",
                    LINK_DOWN = "pid.link.down"
                }
            }

            -- Create channel mock
            local call_count = 0
            mock(_G, "channel", {
                select = function(cases)
                    call_count = call_count + 1

                    -- First call: simulate event
                    if call_count == 1 then
                        return {
                            ok = true,
                            channel = events_channel,
                            value = mock_event
                        }
                        -- Second call: exit the loop
                    else
                        return { ok = false }
                    end
                end
            })

            -- Create the actor with dependency injection
            local test_actor = actor.new(
                { value = 0 },
                {
                    __on_event = function(state, event)
                        handler_called = true
                        expect(event.kind).to_equal("pid.exit", "Event should have correct kind")
                        expect(event.from).to_equal("other_pid", "Event should have correct source")
                        expect(event.result.value.status).to_equal("completed", "Event should have correct result")
                        state.value = 99
                        return nil
                    end
                },
                mock_proc
            )

            -- Run the actor
            test_actor.run()

            -- Verify handler was called
            expect(handler_called).to_be_true("Event handler should have been called")
        end)

        it("should exit when __on_event returns actor.exit()", function()
            -- Create mock cancel event
            local mock_event = {
                kind = "pid.cancel",
                from = "parent_pid",
                at = time.now(),
                deadline = time.now():add(5, "s")
            }

            -- Create mock channels
            local inbox_channel = {
                case_receive = function() return "inbox_case" end
            }

            local events_channel = {
                case_receive = function() return "events_case" end
            }

            -- Create a mock process implementation
            local mock_proc = {
                inbox = function() return inbox_channel end,
                events = function() return events_channel end,
                listen = function(topic)
                    return {
                        case_receive = function() return topic .. "_case" end
                    }
                end,
                send = function(dest, topic, payload) return true end,
                pid = function() return "test-pid" end,
                event = {
                    CANCEL = "pid.cancel",
                    EXIT = "pid.exit",
                    LINK_DOWN = "pid.link.down"
                }
            }

            -- Create channel mock that will simulate a cancel event
            mock(_G, "channel", {
                select = function(cases)
                    return {
                        ok = true,
                        channel = events_channel,
                        value = mock_event
                    }
                end
            })

            local exit_called = false

            -- Create the actor with dependency injection
            local test_actor = actor.new(
                { value = 0 },
                {
                    __on_event = function(state, event)
                        if event.kind == "pid.cancel" then
                            exit_called = true
                            -- Exit the actor with a result
                            return actor.exit({ status = "cancelled", final_value = state.value })
                        end
                    end
                },
                mock_proc
            )

            -- Run the actor
            local result = test_actor.run()

            -- Verify exit was called
            expect(exit_called).to_be_true("Exit should have been called")

            -- Verify proper exit
            expect(result).not_to_be_nil("Actor should return a result when exiting")
            expect(result.status).to_equal("cancelled", "Actor should exit with cancelled status")
            expect(result.final_value).to_equal(0, "Actor should return state values in exit result")
        end)
    end)
end

return require("test").run_cases(define_tests)