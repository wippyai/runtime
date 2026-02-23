-- SPDX-License-Identifier: MPL-2.0

-- Simple worker that processes data and exits with result

local function main(args)
	local work_data = args.work_data or ""

	-- Simulate some work
	local result = "processed: " .. work_data

	return result
end

return { main = main }
