local uuid = require("uuid")

local function test_v4(): boolean
	local id: string = uuid.v4()
	return #id == 36 and id:match("^%x%x%x%x%x%x%x%x%-%x%x%x%x%-%x%x%x%x%-%x%x%x%x%-%x%x%x%x%x%x%x%x%x%x%x%x$") ~= nil
end

local function test_v7(): boolean
	local id: string = uuid.v7()
	return #id == 36
end

local function test_uniqueness(): boolean
	local id1: string = uuid.v4()
	local id2: string = uuid.v4()
	local id3: string = uuid.v4()
	return id1 ~= id2 and id2 ~= id3 and id1 ~= id3
end

local function main(): boolean
	return test_v4() and test_v7() and test_uniqueness()
end

return { main = main }
