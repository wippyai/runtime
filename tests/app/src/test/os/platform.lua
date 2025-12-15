local assert = require("assert_primitives")

local function main()
    -- os.platform should be "wippy"
    assert.is_string(os.platform, "os.platform should be a string")
    assert.eq(os.platform, "wippy", "os.platform should be 'wippy'")

    return {success = true}
end

return {main = main}
