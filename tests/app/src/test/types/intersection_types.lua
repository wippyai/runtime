type Named = { name: string }
type Aged = { age: number}
type Person = Named & Aged

local function greet_person(p: Person): string
	return "Hello " .. p.name .. ", age " .. tostring(p.age)
end

local function main(): boolean
	local p: Person = { name = "Alice", age = 30 }
	local msg: string = greet_person(p)

	return msg == "Hello Alice, age 30" and p.name == "Alice" and p.age == 30
end

return { main = main }
