-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local time = require("time")

type Message = process.Message
type Event = process.Event
type MessageChannel = Channel<Message>
type EventChannel = Channel<Event>

local function wait_for_exit(events_ch: EventChannel, pid: string, timeout: string?)
	local deadline = time.after(timeout or "5s")
	while true do
		local result = channel.select {
			events_ch:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, "timeout waiting for exit"
		end
		local event = result.value as Event
		if event.from == pid and event.kind == process.event.EXIT then
			return event, nil
		end
	end
end

local function wait_for_topic(inbox: MessageChannel, pid: string, topic: string, timeout: string?)
	local deadline = time.after(timeout or "5s")
	while true do
		local result = channel.select {
			inbox:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, "timeout waiting for message"
		end
		local msg = result.value as Message
		if msg:from() == pid and msg:topic() == topic then
			return msg, nil
		end
	end
end

local function payload_data(msg: Message): any?
	local p = msg:payload()
	return p and p:data() or nil
end

local function main()
	local inbox = process.inbox()
	local events_ch = process.events()
	assert.not_nil(inbox, "got inbox channel")
	assert.not_nil(events_ch, "got events channel")

	local pid, err = process
		.with_options({})
		:spawn_monitored(
			"app.test.temporal.workflows:signal_updates_workflow",
			"app.test.temporal:test_worker",
			{ initial = 2 }
		)

	assert.is_nil(err, "spawn signal-updates workflow no error")
	assert.is_string(pid, "got pid")

	local ok, send_err = process.send(pid, "increment", { amount = 3 })
	assert.is_nil(send_err, "increment send ok")
	assert.eq(ok, true, "increment send returns true")

	local msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for increment ack")
	end
	assert.eq(msg:topic(), "ack", "increment ack topic")

	msg, wait_err = wait_for_topic(inbox, pid, "ok", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for increment ok")
	end
	local data = payload_data(msg)
	if type(data) ~= "table" then
		error("increment ok payload must be table")
	end
	assert.eq(data.value, 5, "incremented value")

	ok, send_err = process.send(pid, "decrement", { amount = 2 })
	assert.is_nil(send_err, "decrement send ok")
	assert.eq(ok, true, "decrement send returns true")

	msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for decrement ack")
	end
	assert.eq(msg:topic(), "ack", "decrement ack topic")

	msg, wait_err = wait_for_topic(inbox, pid, "ok", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for decrement ok")
	end
	data = payload_data(msg)
	if type(data) ~= "table" then
		error("decrement ok payload must be table")
	end
	assert.eq(data.value, 3, "decremented value")

	ok, send_err = process.send(pid, "decrement", { amount = 10 })
	assert.is_nil(send_err, "invalid decrement send ok")
	assert.eq(ok, true, "invalid decrement send returns true")

	msg, wait_err = wait_for_topic(inbox, pid, "nak", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for decrement nak")
	end
	data = payload_data(msg)
	assert.contains(tostring(data), "negative", "negative reject reason")

	ok, send_err = process.send(pid, "increment", { amount = "bad" })
	assert.is_nil(send_err, "invalid increment send ok")
	assert.eq(ok, true, "invalid increment send returns true")

	msg, wait_err = wait_for_topic(inbox, pid, "nak", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for increment nak")
	end
	data = payload_data(msg)
	assert.contains(tostring(data), "amount must be a number", "amount validation")

	ok, send_err = process.send(pid, "get_value", {})
	assert.is_nil(send_err, "get_value send ok")
	assert.eq(ok, true, "get_value send returns true")

	msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for get_value ack")
	end
	assert.eq(msg:topic(), "ack", "get_value ack")

	msg, wait_err = wait_for_topic(inbox, pid, "ok", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for get_value ok")
	end
	data = payload_data(msg)
	if type(data) ~= "table" then
		error("get_value ok payload must be table")
	end
	assert.eq(data.value, 3, "get_value returns current counter")

	ok, send_err = process.send(pid, "fail", {})
	assert.is_nil(send_err, "fail send ok")
	assert.eq(ok, true, "fail send returns true")

	msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for fail ack")
	end
	assert.eq(msg:topic(), "ack", "fail ack")

	msg, wait_err = wait_for_topic(inbox, pid, "error", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for fail error")
	end
	data = payload_data(msg)
	assert.contains(tostring(data), "intentional failure", "fail error payload")

	ok, send_err = process.send(pid, "finish", {})
	assert.is_nil(send_err, "finish send ok")
	assert.eq(ok, true, "finish send returns true")

	msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for finish ack")
	end
	assert.eq(msg:topic(), "ack", "finish ack")

	msg, wait_err = wait_for_topic(inbox, pid, "ok", "5s")
	assert.is_nil(wait_err, wait_err)
	if msg == nil then
		error("missing message for finish ok")
	end
	data = payload_data(msg)
	if type(data) ~= "table" then
		error("finish ok payload must be table")
	end
	assert.contains(tostring(data.message), "finishing", "finish message")

	local event, exit_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(exit_err, exit_err)
	if event == nil then
		error("missing exit event")
	end
	assert.eq(event.from, pid, "exit from pid")
	assert.is_table(event.result.value, "result value table")
	assert.eq(event.result.value.final_counter, 3, "final counter")
	assert.eq(event.result.value.updates_processed, 2, "only valid updates counted")

	return true
end

return { main = main }
