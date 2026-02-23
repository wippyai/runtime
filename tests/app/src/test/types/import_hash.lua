-- SPDX-License-Identifier: MPL-2.0

local hash = require("hash")

local function test_md5(): boolean
	local input: string = "hello"
	local result: string = hash.md5(input)
	return #result == 32
end

local function test_sha256(): boolean
	local input: string = "hello"
	local result: string = hash.sha256(input)
	return #result == 64
end

local function test_sha512(): boolean
	local input: string = "hello"
	local result: string = hash.sha512(input)
	return #result == 128
end

local function test_hmac(): boolean
	local key: string = "secret"
	local data: string = "message"
	local result: string = hash.hmac_sha256(key, data)
	return #result == 64
end

local function main(): boolean
	return test_md5() and test_sha256() and test_sha512() and test_hmac()
end

return { main = main }
