-- SPDX-License-Identifier: MPL-2.0

-- Workflow that handles signal messages as update-like requests

local function main(input)
	local counter = input.initial or 0
	local processed_updates = {}
	local done = false

	-- Listen for increment updates
	coroutine.spawn(function()
		local ch = process.listen("increment", {message = true})
		while not done do
			local msg, ok = ch:receive()
			if not ok then
				break
			end

			local p = msg:payload()
			local data = p and p:data() or nil
			local reply_to = msg:from()

			if type(data) ~= "table" or type(data.amount) ~= "number" then
				process.send(reply_to, "nak", "amount must be a number")
			else
				process.send(reply_to, "ack")
				counter = counter + data.amount
				table.insert(processed_updates, {
					type = "increment",
					amount = data.amount,
					new_value = counter
				})
				process.send(reply_to, "ok", {value = counter})
			end
		end
	end)

	-- Listen for decrement updates
	coroutine.spawn(function()
		local ch = process.listen("decrement", {message = true})
		while not done do
			local msg, ok = ch:receive()
			if not ok then
				break
			end

			local p = msg:payload()
			local data = p and p:data() or nil
			local reply_to = msg:from()

			if type(data) ~= "table" or type(data.amount) ~= "number" then
				process.send(reply_to, "nak", "amount must be a number")
			elseif counter - data.amount < 0 then
				process.send(reply_to, "nak", "would result in negative value")
			else
				process.send(reply_to, "ack")
				counter = counter - data.amount
				table.insert(processed_updates, {
					type = "decrement",
					amount = data.amount,
					new_value = counter
				})
				process.send(reply_to, "ok", {value = counter})
			end
		end
	end)

	-- Listen for get_value updates
	coroutine.spawn(function()
		local ch = process.listen("get_value", {message = true})
		while not done do
			local msg, ok = ch:receive()
			if not ok then
				break
			end
			local reply_to = msg:from()
			process.send(reply_to, "ack")
			process.send(reply_to, "ok", {value = counter})
		end
	end)

	-- Listen for fail updates
	coroutine.spawn(function()
		local ch = process.listen("fail", {message = true})
		while not done do
			local msg, ok = ch:receive()
			if not ok then
				break
			end
			local reply_to = msg:from()
			process.send(reply_to, "ack")
			process.send(reply_to, "error", "intentional failure for testing")
		end
	end)

	-- Listen for finish updates (main coroutine)
	local finish_ch = process.listen("finish", {message = true})
	local msg, ok = finish_ch:receive()
	if ok then
		local reply_to = string(msg:from())
		process.send(reply_to, "ack")
		process.send(reply_to, "ok", {message = "finishing"})
	end
	done = true

	return {
		final_counter = counter,
		updates_processed = #processed_updates,
		history = processed_updates
	}
end

return main
