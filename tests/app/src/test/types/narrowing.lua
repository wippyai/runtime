-- SPDX-License-Identifier: MPL-2.0

local function nil_check(x: number?): number
	if x ~= nil then
		return x
	end
	return 0
end

local function type_guard(x: number | string): string
	if type(x) == "number" then
		return "got number: " .. tostring(x)
	else
		return "got string: " .. x
	end
end

local function or_narrowing(x: string?): string
	return x or "default"
end

local function main(): boolean
	local a: number = nil_check(42)
	local b: number = nil_check(nil)

	local c: string = type_guard(100)
	local d: string = type_guard("hello")

	local e: string = or_narrowing("value")
	local f: string = or_narrowing(nil)

	return a == 42 and b == 0 and
	c == "got number: 100" and d == "got string: hello" and
	e == "value" and f == "default"
end

return { main = main }
