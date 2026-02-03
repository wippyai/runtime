-- Test: json encode/decode round trip
local assert = require("assert_primitives")

local function main()
	local json = require("json")

	-- Simple object round trip
	local original = {name = "test", value = 123, active = true}
	local encoded = json.encode(original)
	local decoded = json.decode(encoded)
	assert.eq(decoded.name, "test", "name preserved")
	assert.eq(decoded.value, 123, "value preserved")
	assert.eq(decoded.active, true, "active preserved")

	-- Array round trip
	local arr = {1, 2, 3, 4, 5}
	local arr_enc = json.encode(arr)
	local arr_dec = json.decode(arr_enc)
	assert.eq(arr_dec[1], 1, "first element preserved")
	assert.eq(arr_dec[5], 5, "last element preserved")

	-- Nested structure round trip
	local nested = {
		items = {1, 2, 3},
		meta = {
			count = 3,
			valid = true
		}
	}
	local nested_enc = json.encode(nested)
	local nested_dec = json.decode(nested_enc)
	assert.eq(nested_dec.items[1], 1, "nested array element")
	assert.eq(nested_dec.meta.count, 3, "nested object field")
	assert.eq(nested_dec.meta.valid, true, "nested boolean field")

	-- String with special characters
	local special = {text = "hello\nworld"}
	local special_enc = json.encode(special)
	local special_dec = json.decode(special_enc)
	assert.eq(special_dec.text, "hello\nworld", "newline preserved")

	return true
end

return { main = main }
