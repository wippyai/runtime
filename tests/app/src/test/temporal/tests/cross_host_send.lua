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
			return nil, "timeout waiting for topic " .. topic
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
	local inbox = process.inbox() as MessageChannel
	local events_ch = process.events() as EventChannel
	assert.not_nil(inbox, "got inbox channel")
	assert.not_nil(events_ch, "got events channel")

	local self_pid = process.pid()
	assert.is_string(self_pid, "self pid string")

	local workflow_pid, spawn_err = process.spawn_monitored(
		"app.test.temporal.workflows:workflow_cross_host_send_workflow",
		"app.test.temporal:test_worker",
		{ target_pid = self_pid }
	)
	assert.is_nil(spawn_err, "spawn cross-host workflow no error")
	assert.is_string(workflow_pid, "workflow pid string")

	local ping_msg, ping_err = wait_for_topic(inbox, workflow_pid, "cross_host_ping", "5s")
	assert.is_nil(ping_err, ping_err)
	if ping_msg == nil then
		error("missing cross_host_ping")
	end
	local ping_data = payload_data(ping_msg)
	assert.is_table(ping_data, "ping payload table")
	assert.eq(ping_data.from, workflow_pid, "ping payload from workflow pid")

	local ok, send_err = process.send(workflow_pid, "cross_host_pong", {
		ack = true,
		from = self_pid,
		probe = "host->workflow",
	})
	assert.is_nil(send_err, "send cross_host_pong no error")
	assert.eq(ok, true, "send cross_host_pong returns true")

	local event, wait_err = wait_for_exit(events_ch, workflow_pid, "5s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing workflow exit event")
	end
	assert.is_nil(event.result.error, "workflow exits without error")
	assert.is_table(event.result.value, "workflow result table")
	assert.eq(event.result.value.ok, true, "workflow roundtrip result ok")
	assert.eq(event.result.value.received_topic, "cross_host_pong", "workflow received pong topic")
	assert.eq(event.result.value.received_from, self_pid, "workflow received reply from host pid")
	assert.is_table(event.result.value.received_payload, "workflow received payload table")
	assert.eq(event.result.value.received_payload.ack, true, "workflow received ack payload")

	return true
end

return { main = main }
