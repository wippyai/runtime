-- SPDX-License-Identifier: MPL-2.0

local function main(name)
	return "Hello, " .. (name or "Anonymous") .. "!"
end

return { main = main }
