-- Function that raises error for testing error propagation

local function main(should_error, message)
	if should_error then
		error("test error: " .. (message or "intentional failure"))
	end
	return { ok = true }
end

return { main = main }
