-- SPDX-License-Identifier: MPL-2.0

local function get_name(person: {name: string, age: number}): string
	return person.name
end

local function make_point(x: number, y: number): {x: number, y: number}
	return { x = x, y = y }
end

local function main(): boolean
	local person: {name: string, age: number} = { name = "Alice", age = 30 }
	local point: {x: number, y: number} = make_point(10, 20)

	local name: string = get_name(person)

	return name == "Alice" and point.x == 10 and point.y == 20
end

return { main = main }
