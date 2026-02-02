local assert = require("assert2")
local logger = require("logger")

local function main()
-- create child logger with fields (colon syntax)
	local child = logger:with({component = "auth", version = "1.0"})
	assert.not_nil(child, "child logger created")

	-- child logger can log
	child:info("login attempt", {user = "test"})
	child:debug("checking credentials")
	child:warn("rate limit approaching")
	child:error("login failed", {reason = "invalid password"})

	-- chain with calls
	local grandchild = child:with({request_id = "abc123"})
	assert.not_nil(grandchild, "grandchild logger created")
	grandchild:info("processing request")

	return true
end

return { main = main }
