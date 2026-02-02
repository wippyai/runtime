local assert = require("assert_primitives")

local function main()
	local time = require("time")

	local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)

	local d = time.parse_duration("1h")
	local added = t:add(d)
	assert.eq(added:hour(), 16, "add duration")

	local added_str = t:add("30m")
	assert.eq(added_str:minute(), 34, "add string")

	local added_num = t:add(time.HOUR)
	assert.eq(added_num:hour(), 16, "add number")

	local t2 = time.date(2024, 12, 29, 14, 4, 5, 0, time.utc)
	local diff = t:sub(t2)
	assert.eq(diff:hours(), 1, "sub times")

	local added_date = t:add_date(1, 2, 3)
	assert.eq(added_date:year(), 2026, "add_date year")
	assert.eq(added_date:month(), 3, "add_date month")

	local t1 = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
	local t3 = time.date(2024, 1, 2, 0, 0, 0, 0, time.utc)
	assert.ok(t1:before(t3), "before")
	assert.ok(t3:after(t1), "after")
	assert.ok(t1:equal(t1), "equal same")
	assert.ok(not t1:equal(t3), "not equal different")

	local formatted = t:format("Mon Jan 2 15:04:05 MST 2006")
	assert.eq(formatted, "Sun Dec 29 15:04:05 UTC 2024", "format")

	local rfc = t:format_rfc3339()
	assert.ok(rfc:find("2024"), "format_rfc3339 contains year")

	local epoch = time.date(1970, 1, 1, 0, 0, 1, 0, time.utc)
	assert.eq(epoch:unix(), 1, "unix")

	local epoch_ns = time.date(1970, 1, 1, 0, 0, 0, 1, time.utc)
	assert.eq(epoch_ns:unix_nano(), 1, "unix_nano")

	local y, m, day = t:date()
	assert.eq(y, 2024, "date year")
	assert.eq(m, 12, "date month")
	assert.eq(day, 29, "date day")

	local h, min, s = t:clock()
	assert.eq(h, 15, "clock hour")
	assert.eq(min, 4, "clock minute")
	assert.eq(s, 5, "clock second")

	local loc = t:location()
	assert.eq(tostring(loc), "UTC", "location")

	local utc = t:utc()
	assert.eq(tostring(utc:location()), "UTC", "utc")

	assert.ok(not t:is_zero(), "not zero")

	local t_round = time.parse("2006-01-02T15:04:05.999999999Z", "2024-01-01T12:34:56.789Z")
	local rounded = t_round:round(time.parse_duration("1s"))
	assert.eq(rounded:format("2006-01-02T15:04:05Z"), "2024-01-01T12:34:57Z", "round")

	local truncated = t_round:truncate(time.parse_duration("1m"))
	assert.eq(truncated:format("2006-01-02T15:04:05Z"), "2024-01-01T12:34:00Z", "truncate")

	return true
end

return { main = main }
