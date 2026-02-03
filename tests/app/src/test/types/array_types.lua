local function sum(arr: {number}): number
	local total: number = 0
	for _, v in ipairs(arr) do
		total = total + v
	end
	return total
end

local function first(arr: {string}): string?
	return arr[1]
end

local function main(): boolean
	local nums: {number} = {1, 2, 3, 4, 5}
	local strs: {string} = {"a", "b", "c"}
	local empty: {integer} = {}

	local total: number = sum(nums)
	local f: string? = first(strs)

	return total == 15 and f == "a" and #empty == 0
end

return { main = main }
