local function new_counter(initial: number): {value: number}
	local counter = {
		value = initial
	}
	return counter
end

local function increment(c: {value: number})
	c.value = c.value + 1
end

local function get_value(c: {value: number}): number
	return c.value
end

local function main(): boolean
	local c = new_counter(10)

	increment(c)
	increment(c)
	increment(c)

	local val: number = get_value(c)

	return val == 13
end

return { main = main }
