local function main(): boolean
	local x: number = 42
	local y: string = "hello"
	local z: boolean = true
	local i: integer = 10

	return x == 42 and y == "hello" and z == true and i == 10
end

return { main = main }
