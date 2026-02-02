-- Workflow that generates UUIDs using side effects
-- Tests deterministic replay of non-deterministic operations
local uuid = require("uuid")

local function main(input)
	local count = input.count or 5
	local uuids = {}

	-- Generate UUIDs using side effects (deterministic in workflow context)
	for _ = 1, count do
		local id, err = uuid.v4()
		if err then
			return nil, err
		end
		uuids[i] = id
	end

	-- Return the generated UUIDs
	return {
		uuids = uuids,
		count = count,
		status = "completed"
	}
end

return main
