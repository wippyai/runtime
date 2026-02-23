-- SPDX-License-Identifier: MPL-2.0

local function count_keys(map: {[string]: number}): integer
	local count: integer = 0
	for _ in pairs(map) do
		count = count + 1
	end
	return count
end

local function main(): boolean
	local scores: {[string]: number} = { alice = 100, bob = 85, carol = 92 }
	local lookup: {[number]: string} = { [1] = "one", [2] = "two" }

	local n: integer = count_keys(scores)
	local val: string? = lookup[1]

	return n == 3 and val == "one"
end

return { main = main }
