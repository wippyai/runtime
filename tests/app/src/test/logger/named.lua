local assert = require("assert2")
local logger = require("logger")

local function main()
-- create named logger (colon syntax)
	local named = logger:named("myservice")
	assert.not_nil(named, "named logger created")

	-- named logger can log
	named:info("service started")
	named:debug("initializing components")

	-- chain named calls
	local sub = named:named("handler")
	assert.not_nil(sub, "sub-named logger created")
	sub:info("handling request")

	-- combine with and named
	local combined = named:with({env = "prod"})
	assert.not_nil(combined, "combined logger created")
	combined:info("combined logging")

	return true
end

return { main = main }
