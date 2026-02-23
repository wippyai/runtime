-- SPDX-License-Identifier: MPL-2.0

local json = require("json")

local function test_encode(): boolean
	local data: {name: string, age: number} = { name = "Alice", age = 30 }
	local encoded: string, err = json.encode(data)
	return err == nil and encoded:find("Alice") ~= nil
end

local function test_decode(): boolean
	local text: string = '{"x": 10, "y": 20}'
	local decoded: any, err = json.decode(text)
	return err == nil and decoded.x == 10 and decoded.y == 20
end

local function test_encode_array(): boolean
	local arr: {number} = {1, 2, 3}
	local encoded: string, err = json.encode(arr)
	return err == nil and encoded == "[1,2,3]"
end

local function main(): boolean
	return test_encode() and test_decode() and test_encode_array()
end

return { main = main }
