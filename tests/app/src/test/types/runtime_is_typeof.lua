local assert = require("assert2")

local sample: {name: string, age: number} = {name = "Ada", age = 33}
local nested: {meta: {active: boolean}, tags: {string}} = {meta = {active = true}, tags = {"x", "y"}}

type Sample = typeof(sample)
type Nested = typeof(nested)

local function main(): boolean
	local ok, err = Sample:is({name = "Bob", age = 1})
	assert.not_nil(ok, "Sample valid should pass")
	assert.is_nil(err, "Sample valid should have nil error")

	local bad, bad_err = Sample:is({name = 123, age = 1})
	assert.is_nil(bad, "Sample wrong name type should fail")
	assert.not_nil(bad_err, "Sample wrong name type should return error")
	assert.error_contains(bad_err, "name", "Sample error should mention name")

	local missing, missing_err = Sample:is({name = "Bob"})
	assert.is_nil(missing, "Sample missing age should fail")
	assert.not_nil(missing_err, "Sample missing age should return error")
	assert.error_contains(missing_err, "age", "Sample error should mention age")

	local nested_ok, nested_err = Nested:is({meta = {active = false}, tags = {"z"}})
	assert.not_nil(nested_ok, "Nested valid should pass")
	assert.is_nil(nested_err, "Nested valid should have nil error")

	local nested_bad, nested_bad_err = Nested:is({meta = {active = "yes"}, tags = {"z"}})
	assert.is_nil(nested_bad, "Nested wrong meta type should fail")
	assert.not_nil(nested_bad_err, "Nested wrong meta type should return error")
	assert.error_contains(nested_bad_err, "meta", "Nested error should mention meta")

	return true
end

return { main = main }
