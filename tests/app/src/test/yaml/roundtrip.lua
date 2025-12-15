-- Test: yaml encode/decode round-trip
local assert = require("assert_primitives")

local function main()
    local yaml = require("yaml")

    -- Simple table round-trip
    local original1 = {name = "test", value = 123}
    local encoded1 = yaml.encode(original1)
    local decoded1 = yaml.decode(encoded1)
    assert.eq(decoded1.name, original1.name, "name preserved")
    assert.eq(decoded1.value, original1.value, "value preserved")

    -- Array round-trip
    local original2 = {1, 2, 3, 4, 5}
    local encoded2 = yaml.encode(original2)
    local decoded2 = yaml.decode(encoded2)
    assert.eq(#decoded2, 5, "array length preserved")
    assert.eq(decoded2[1], 1, "array first element preserved")
    assert.eq(decoded2[5], 5, "array last element preserved")

    -- Nested structure round-trip
    local original3 = {
        parent = {
            child = {
                value = 42
            }
        }
    }
    local encoded3 = yaml.encode(original3)
    local decoded3 = yaml.decode(encoded3)
    assert.eq(decoded3.parent.child.value, 42, "nested value preserved")

    -- Mixed types round-trip
    local original4 = {
        str = "hello world",
        num = 3.14,
        bool = true,
        nested = {a = 1, b = 2}
    }
    local encoded4 = yaml.encode(original4)
    local decoded4 = yaml.decode(encoded4)
    assert.eq(decoded4.str, original4.str, "string preserved")
    assert.eq(decoded4.num, original4.num, "number preserved")
    assert.eq(decoded4.bool, original4.bool, "boolean preserved")
    assert.eq(decoded4.nested.a, 1, "nested map preserved")

    return true
end

return { main = main }
