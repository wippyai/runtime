-- Process that accepts multiple arguments
local function main(a, b, c)
	return {
		args = { a = a, b = b, c = c },
		count = (a and 1 or 0) + (b and 1 or 0) + (c and 1 or 0)
	}
end

return { main = main }
