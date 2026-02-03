local function maybe(cond: boolean): number?
	if cond then
		return 42
	end
	return nil
end

local function with_default(val: number?, default: number): number
	return val or default
end

local function main(): boolean
	local x: number? = maybe(true)
	local y: number? = maybe(false)
	local z: number? = nil

	local a: number = with_default(x, 0)
	local b: number = with_default(y, 100)
	local c: number = with_default(z, 200)

	return a == 42 and b == 100 and c == 200
end

return { main = main }
