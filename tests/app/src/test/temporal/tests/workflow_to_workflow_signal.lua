local assert = require("assert2")
local time = require("time")

local function wait_for_exits(events_ch, pids, timeout)
	local deadline = time.after(timeout or "5s")
	local pending = {}
	local exits = {}
	for i = 1, #pids do
		pending[pids[i]] = true
	end

	while true do
		local has_pending = false
		for _, is_pending in pairs(pending) do
			if is_pending then
				has_pending = true
				break
			end
		end
		if not has_pending then
			return exits, nil
		end

		local result = channel.select {
			events_ch:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, "timeout waiting for exits"
		end
		local event = result.value
		if event.kind == process.event.EXIT and pending[event.from] then
			exits[event.from] = event
			pending[event.from] = false
		end
	end
end

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	local target_pid, err = process.spawn_monitored(
		"app.test.temporal.workflows:inbox_workflow",
		"app.test.temporal:test_worker"
	)
	assert.is_nil(err, "spawn target workflow no error")
	assert.is_string(target_pid, "got target pid")

	-- Give target workflow a brief head start before external signal.
	time.sleep(100 * time.MILLISECOND)

	local sender_pid, sender_err = process.spawn_monitored(
		"app.test.temporal.workflows:workflow_signal_peer_workflow",
		"app.test.temporal:test_worker",
		{
			target_pid = target_pid,
			topic = "peer_ping",
			payload = { source = "peer_workflow" }
		}
	)
	assert.is_nil(sender_err, "spawn sender workflow no error")
	assert.is_string(sender_pid, "got sender pid")

	local exits, exits_err = wait_for_exits(events_ch, { sender_pid, target_pid }, "5s")
	assert.is_nil(exits_err, exits_err)
	if exits == nil then
		error("missing exits table")
	end

	local sender_exit = exits[sender_pid]
	if sender_exit == nil then
		error("missing sender exit event")
	end
	assert.is_table(sender_exit.result.value, "sender result value table")
	assert.eq(sender_exit.result.value.ok, true, "sender reported send success")

	local target_exit = exits[target_pid]
	if target_exit == nil then
		error("missing target exit event")
	end

	assert.is_table(target_exit.result.value, "target result value table")
	assert.eq(target_exit.result.value.received_topic, "peer_ping", "target received peer topic")
	assert.is_table(target_exit.result.value.received_payload, "target received payload table")
	assert.eq(target_exit.result.value.received_payload.source, "peer_workflow", "target payload source")

	return true
end

return { main = main }
