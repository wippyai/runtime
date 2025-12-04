-- Test: HTML sanitization policies
local assert = require("assert_primitives")

local function main()
    local html = require("html")

    -- Test new_policy creation
    local policy, err = html.sanitize.new_policy()
    assert.is_nil(err, "new_policy should not error")
    assert.not_nil(policy, "policy returned")

    -- Test ugc_policy creation
    local ugc, err2 = html.sanitize.ugc_policy()
    assert.is_nil(err2, "ugc_policy should not error")
    assert.not_nil(ugc, "ugc policy returned")

    -- Test strict_policy creation
    local strict, err3 = html.sanitize.strict_policy()
    assert.is_nil(err3, "strict_policy should not error")
    assert.not_nil(strict, "strict policy returned")

    -- Test strict policy strips all HTML
    local dirty = '<p>Hello <script>alert("xss")</script> world</p>'
    local clean = strict:sanitize(dirty)
    assert.eq(clean, "Hello  world", "strict policy strips all HTML")

    -- Test UGC policy allows basic formatting
    local ugc_input = '<p><strong>Bold</strong> and <em>italic</em></p>'
    local ugc_result = ugc:sanitize(ugc_input)
    assert.contains(ugc_result, "<strong>", "UGC allows strong")
    assert.contains(ugc_result, "<em>", "UGC allows em")

    -- Test XSS prevention
    local xss_input = '<img src="x" onerror="alert(1)">'
    local xss_clean = strict:sanitize(xss_input)
    assert.ok(not string.find(xss_clean, "onerror"), "XSS attribute stripped")

    return true
end

return { main = main }
