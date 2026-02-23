-- SPDX-License-Identifier: MPL-2.0

local function get_first(arr: readonly {number}): number?
	return arr[1]
end

local function get_value(map: readonly {[string]: number}, key: string): number?
	return map[key]
end

local function main(): boolean
	local nums: readonly {number} = {10, 20, 30}
	local scores: readonly {[string]: number} = { a = 1, b = 2 }

	local first: number? = get_first(nums)
	local val: number? = get_value(scores, "a")

	return first == 10 and val == 1
end

return { main = main }
