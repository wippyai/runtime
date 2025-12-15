-- Test: error details handling
local assert = require("assert2")

local function main()
    -- Nested details
    local e = errors.new({
        message = "nested",
        details = {
            user = {name = "test", id = 1},
            count = 5
        }
    })

    local d = e:details()
    assert.ok(d, "details exist")
    assert.eq(d.count, 5, "primitive value")
    assert.ok(d.user, "nested table exists")
    assert.eq(d.user.name, "test", "nested field")
    assert.eq(d.user.id, 1, "nested field")

    -- Empty details becomes nil
    local e_empty = errors.new({message = "empty", details = {}})
    assert.is_nil(e_empty:details(), "empty details is nil")

    return true
end

return { main = main }
