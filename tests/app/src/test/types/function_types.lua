-- SPDX-License-Identifier: MPL-2.0

local function apply(f: (number) -> number, x: number): number
	return f(x)
end

local function compose(f: (number) -> number, g: (number) -> number): (number) -> number
	return function(x: number): number
		return f(g(x))
	end
end

local function main(): boolean
	local double: (number) -> number = function(x: number): number
		return x * 2
	end

	local inc: (number) -> number = function(x: number): number
		return x + 1
	end

	local r1: number = apply(double, 5)
	local composed: (number) -> number = compose(double, inc)
	local r2: number = composed(3)

	return r1 == 10 and r2 == 8
end

return { main = main }
