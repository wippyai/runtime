-- SPDX-License-Identifier: MPL-2.0

-- Worker: Short-lived process that exits immediately
local function main()
	return true
end

return { main = main }
