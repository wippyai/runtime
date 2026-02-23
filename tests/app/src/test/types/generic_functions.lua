-- SPDX-License-Identifier: MPL-2.0

local function identity<T>(x: T): T
	return x
end

local function pair<K, V>(key: K, value: V): {key: K, value: V}
	return { key = key, value = value }
end

local function first<T>(arr: {T}): T?
	return arr[1]
end

local function main(): boolean
	local n: number = identity(42)
	local s: string = identity("hello")

	local p: {key: string, value: number} = pair("age", 30)

	local nums: {number} = {1, 2, 3}
	local f: number? = first(nums)

	return n == 42 and s == "hello" and p.key == "age" and f == 1
end

return { main = main }
