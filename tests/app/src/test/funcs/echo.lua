-- SPDX-License-Identifier: MPL-2.0

-- Simple echo function for testing funcs.call

local function main(input)
	return {
		echo = input or "no input",
		ok = true
	}
end

return { main = main }
