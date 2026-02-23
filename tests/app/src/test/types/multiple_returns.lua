-- SPDX-License-Identifier: MPL-2.0

local function div_mod(a: number, b: number): (number, number)
	return math.floor(a / b), a % b
end

local function find(arr: {string}, target: string): (integer?, string?)
	for i, v in ipairs(arr) do
		if v == target then
			return i, v
		end
	end
	return nil, nil
end

local function parse_pair(s: string): (string, string)
	local k: string?, v: string? = s:match("(%w+)=(%w+)")
	return k or "", v or ""
end

local function main(): boolean
	local q: number, r: number = div_mod(17, 5)

	local arr: {string} = {"a", "b", "c"}
	local idx1: integer?, val1: string? = find(arr, "b")
	local idx2: integer?, val2: string? = find(arr, "x")

	local k: string, v: string = parse_pair("name=value")

	return q == 3 and r == 2 and
	idx1 == 2 and val1 == "b" and
	idx2 == nil and val2 == nil and
	k == "name" and v == "value"
end

return { main = main }
