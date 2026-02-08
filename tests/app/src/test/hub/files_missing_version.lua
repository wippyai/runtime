local function main()
	local assert = assert2

	local res, err = hub.files.list("wippy/terminal", nil)
	assert.has_error(res, err, "version required")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
end

return { main = main }
