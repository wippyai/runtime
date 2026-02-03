local logger = require("logger")

local function main()
-- test string field (colon syntax)
	logger:info("string field", {name = "test"})

	-- test number field
	logger:info("number field", {count = 42})

	-- test float field
	logger:info("float field", {value = 3.14159})

	-- test boolean field
	logger:info("bool field", {enabled = true, disabled = false})

	-- test multiple fields
	logger:info("multiple fields", {
		str = "hello",
		num = 100,
		float = 2.5,
		bool = true
	})

	-- test nested table (converted to any)
	logger:info("nested field", {
		data = {nested = "value"}
	})

	return true
end

return { main = main }
