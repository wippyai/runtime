local function main()
	local assert = assert2

	local res, err = hub.modules.readme({ org = "wippy" })
	assert.has_error(res, err, "missing name should error")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
end

return { main = main }

