-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function test_now(): boolean
	local t = time.now()
	return t ~= nil and type(t) == "userdata"
end

local function test_format(): boolean
	local t = time.now()
	local str: string = t:format(time.RFC3339)
	return type(str) == "string" and #str > 0
end

local function test_unix(): boolean
	local t = time.now()
	local unix: number = t:unix()
	return unix > 0
end

local function test_add(): boolean
	local t1 = time.now()
	local d = time.parse_duration("1h")
	local t2 = t1:add(d)
	return t2:unix() > t1:unix()
end

local function main(): boolean
	return test_now() and test_format() and test_unix() and test_add()
end

return { main = main }
