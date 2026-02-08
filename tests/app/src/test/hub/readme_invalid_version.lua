local function main()
	local assert = assert2

	local res, err = hub.modules.readme("wippy/terminal", { version = { bad = "x" } })
	assert.has_error(res, err, "invalid version ref should error")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
end

return { main = main }
