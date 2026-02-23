-- SPDX-License-Identifier: MPL-2.0

local function main(): boolean
	local x: any = 42
	local y: number = x as number

	local s: any = "hello"
	local t: string = s as string

	local mixed: any = { value = 100 }
	local obj: {value: number} = mixed as {value: number}

	return y == 42 and t == "hello" and obj.value == 100
end

return { main = main }
