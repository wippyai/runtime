-- SPDX-License-Identifier: MPL-2.0

-- Worker that tests inbox fallback behavior
-- Receives messages on both specific topic and inbox, counts them

local time = require("time")

local function main()
-- Get inbox and listen to specific topic
	local inbox_ch = process.inbox()
	if not inbox_ch then
		return false, "inbox returned nil"
	end

	local specific_ch = process.listen("specific_topic")
	if not specific_ch then
		return false, "listen returned nil"
	end

	local specific_count = 0
	local inbox_count = 0

	-- Set a timeout to stop collecting
	local timeout = time.after("1s")

	-- Collect messages until timeout
	while true do
		local result = channel.select({
			specific_ch:case_receive(),
			inbox_ch:case_receive(),
			timeout:case_receive()
		})

		if result.channel == timeout then
			break
		elseif result.channel == specific_ch then
			specific_count = specific_count + 1
		elseif result.channel == inbox_ch then
			inbox_count = inbox_count + 1
		end
	end

	return {
		specific_count = specific_count,
		inbox_count = inbox_count
	}
end

return { main = main }
