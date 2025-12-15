local assert = require("assert_primitives")

local function main()
    local time = require("time")

    local t, err = time.parse("2006-01-02 15:04:05", "2024-12-29 15:04:05")
    assert.is_nil(err, "parse succeeds")
    assert.not_nil(t, "parse returns time")
    assert.eq(t:year(), 2024, "parsed year")
    assert.eq(t:month(), 12, "parsed month")
    assert.eq(t:day(), 29, "parsed day")

    local bad_t, bad_err = time.parse("2006-01-02", "invalid-date")
    assert.is_nil(bad_t, "invalid parse returns nil")
    assert.not_nil(bad_err, "invalid parse returns error")

    local d, d_err = time.parse_duration("1h30m")
    assert.is_nil(d_err, "parse_duration succeeds")
    assert.not_nil(d, "parse_duration returns duration")
    assert.ok(d:hours() > 1.4 and d:hours() < 1.6, "duration hours match")

    local d2, _ = time.parse_duration(time.SECOND)
    assert.eq(d2:seconds(), 1, "parse_duration from number")

    local bad_d, bad_d_err = time.parse_duration("invalid")
    assert.is_nil(bad_d, "invalid duration returns nil")
    assert.not_nil(bad_d_err, "invalid duration returns error")

    return true
end

return { main = main }
