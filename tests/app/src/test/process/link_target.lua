-- SPDX-License-Identifier: MPL-2.0

-- Simple target process for link testing
-- Waits for "exit" message and then exits with error to trigger LINK_DOWN

local time = require("time")

local function main()
	local inbox_ch = process.inbox()
	local timeout = time.after("5s")

	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for exit signal"
	end

	-- Exit with error to trigger LINK_DOWN to linked processes
	-- Normal exit does NOT trigger LINK_DOWN per spec
	error("INTENTIONAL_EXIT_TO_TRIGGER_LINK_DOWN")
end

return { main = main }
