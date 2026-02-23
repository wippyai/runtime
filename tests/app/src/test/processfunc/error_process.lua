-- SPDX-License-Identifier: MPL-2.0

-- Process that returns error
local function main()
	error("intentional process error")
end

return { main = main }
