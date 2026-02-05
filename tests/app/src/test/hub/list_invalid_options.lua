local function main()
	local assert = assert2

	local res, err = hub.modules.list("nope")
	assert.has_error(res, err, "options must be table")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
end

return { main = main }
