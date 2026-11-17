-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main(parent)
	local events = process.events()
	local release = process.listen("release")
	local cases = { events:case_receive(), default = true }
	assert(channel.select(cases).default, "empty events channel should select default")
	assert(process.send(parent, "ready", true))
	local released = channel.select { release:case_receive(), time.after("5s"):case_receive() }
	assert(released.channel == release and released.value == true, "worker was not released")

	for _, reason in ipairs({ "first", "second" }) do
		local event = channel.select(cases)
		assert(event.channel == events and event.ok, "expected " .. reason .. " cancellation on events channel")
		assert(event.value.kind == process.event.CANCEL and event.value.reason == reason, "unexpected cancellation")
	end
	assert(channel.select(cases).default, "drained events channel should select default")
	return true
end

return { main = main }
