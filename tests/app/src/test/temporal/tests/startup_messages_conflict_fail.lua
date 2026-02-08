local assert = require("assert2")
local time = require("time")

type Message = process.Message
type MessageChannel = Channel<Message>
type Event = process.Event
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

local function terminate_best_effort(pid: string?)
	if pid == nil then
		return
	end
	process.terminate(pid)
end

local function main()
	local inbox = process.inbox()
	local events_ch = process.events()
	assert.not_nil(inbox, "got inbox channel")
	assert.not_nil(events_ch, "got events channel")

	local started_pid: string? = nil
	local ok_run, run_err = pcall(function()
		local workflow_name = "startup-conflict-fail-" .. tostring(time.now():unix_nano())

		local first = process
			.with_options({})
			:with_name(workflow_name)
			:with_message("increment", { amount = 3 })

		local pid, err = first:spawn(
			"app.test.temporal.workflows:signal_updates_workflow",
			"app.test.temporal:test_worker",
			{ initial = 0 }
		)
		assert.is_nil(err, "first spawn no error")
		assert.is_string(pid, "first spawn pid")
		started_pid = pid

		local msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
		assert.is_nil(wait_err, wait_err)
		assert.not_nil(msg, "first startup ack received")
		msg, wait_err = wait_for_topic(inbox, pid, "ok", "5s")
		assert.is_nil(wait_err, wait_err)
		assert.not_nil(msg, "first startup ok received")
		local data = payload_data(msg)
		assert.is_table(data, "first startup ok payload table")
		assert.eq(data.value, 3, "first startup increment applied")

		local second = process
			.with_options({
				["temporal.workflow.id_conflict_policy"] = "fail",
			})
			:with_name(workflow_name)
			:with_message("increment", { amount = 2 })

		local pid2, err2 = second:spawn(
			"app.test.temporal.workflows:signal_updates_workflow",
			"app.test.temporal:test_worker",
			{ initial = 999 }
		)
		assert.is_nil(pid2, "second spawn pid nil on conflict fail")
		assert.not_nil(err2, "second spawn returns conflict error")

		local ok, mon_err = process.monitor(pid)
		assert.is_nil(mon_err, "monitor no error")
		assert.eq(ok, true, "monitor returns true")

		local ok, send_err = process.send(pid, "finish", {})
		assert.is_nil(send_err, "finish send ok")
		assert.eq(ok, true, "finish send returns true")

		msg, wait_err = wait_for_topic(inbox, pid, "ack", "5s")
		assert.is_nil(wait_err, wait_err)
		assert.not_nil(msg, "finish ack received")
		msg, wait_err = wait_for_topic(inbox, pid, "ok", "5s")
		assert.is_nil(wait_err, wait_err)
		assert.not_nil(msg, "finish ok received")

		local event, wait_err = wait_for_exit(events_ch, pid, "5s")
		assert.is_nil(wait_err, wait_err)
		if event == nil then
			error("missing exit event")
		end

		started_pid = nil
	end)

	if not ok_run then
		terminate_best_effort(started_pid)
		error(run_err)
	end

	return true
end

return { main = main }
