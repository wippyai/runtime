local function sum(...: number): number
	local total: number = 0
	for _, v in ipairs({...}) do
		total = total + v
	end
	return total
end

local function format(fmt: string, ...: any): string
	return string.format(fmt, ...)
end

local function count(...: any): number
	return select("#", ...)
end

local function main(): boolean
	local s1: number = sum(1, 2, 3)
	local s2: number = sum(10, 20)
	local s3: number = sum()

	local f1: string = format("x=%d y=%d", 10, 20)
	local f2: string = format("hello %s", "world")

	local c1: integer = count(1, 2, 3, 4, 5)
	local c2: integer = count()

	return s1 == 6 and s2 == 30 and s3 == 0 and
	f1 == "x=10 y=20" and f2 == "hello world" and
	c1 == 5 and c2 == 0
end

return { main = main }
