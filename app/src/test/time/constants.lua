local assert = require("assert_primitives")

local function main()
    local time = require("time")

    assert.eq(time.NANOSECOND, 1, "NANOSECOND")
    assert.eq(time.MICROSECOND, 1000, "MICROSECOND")
    assert.eq(time.MILLISECOND, 1000000, "MILLISECOND")
    assert.eq(time.SECOND, 1000000000, "SECOND")
    assert.eq(time.MINUTE, 60000000000, "MINUTE")
    assert.eq(time.HOUR, 3600000000000, "HOUR")

    assert.eq(time.RFC3339, "2006-01-02T15:04:05Z07:00", "RFC3339")
    assert.eq(time.DATE_TIME, "2006-01-02 15:04:05", "DATE_TIME")
    assert.eq(time.DATE_ONLY, "2006-01-02", "DATE_ONLY")
    assert.eq(time.TIME_ONLY, "15:04:05", "TIME_ONLY")

    assert.eq(time.JANUARY, 1, "JANUARY")
    assert.eq(time.DECEMBER, 12, "DECEMBER")

    assert.eq(time.SUNDAY, 0, "SUNDAY")
    assert.eq(time.SATURDAY, 6, "SATURDAY")

    return true
end

return { main = main }
