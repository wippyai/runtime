-- SPDX-License-Identifier: MPL-2.0

local function main(input)
	if input == nil then
		return { message = "no input provided", status = "error" }
	end

	local id = input.id or "unknown"
	local name = input.name or "unknown"

	return {
		message = string.format("Processed: id=%s, name=%s", id, name),
		processed_id = id,
		processed_name = name,
		status = "success"
	}
end

return main
