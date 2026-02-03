local function process(val: number | string): string
	if type(val) == "number" then
		return "number:" .. tostring(val)
	else
		return "string:" .. val
	end
end

local function main(): boolean
	local a: number | string = 42
	local b: number | string = "hello"
	local _: number | string | boolean = true

	local ra: string = process(a)
	local rb: string = process(b)

	return ra == "number:42" and rb == "string:hello"
end

return { main = main }
