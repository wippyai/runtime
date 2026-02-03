-- Simple process that returns input as result
local function main(input)
	return {
		echo = input or "no input",
		ok = true
	}
end

return { main = main }
