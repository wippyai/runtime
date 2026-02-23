-- SPDX-License-Identifier: MPL-2.0

-- Worker: Sends message to parent then exits (for channel identity test)
local function main(parent_pid: string)
	if not parent_pid then
		error("parent_pid required")
	end

	local _, err = process.send(parent_pid, "inbox", "hello")
	if err then
		error("send failed: " .. tostring(err))
	end

	return true
end

return { main = main }
