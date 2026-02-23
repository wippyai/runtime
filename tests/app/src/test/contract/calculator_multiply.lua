-- SPDX-License-Identifier: MPL-2.0

local function main(a, b)
	return (a or 0) * (b or 0)
end

return { main = main }
