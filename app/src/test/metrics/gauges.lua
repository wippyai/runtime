local assert = require("assert_primitives")

local function main()
    local metrics = require("metrics")

    -- gauge_set
    local ok, err = metrics.gauge_set("test.gauge.set", 42)
    assert.eq(ok, true, "gauge_set returns true")
    assert.is_nil(err, "gauge_set no error")

    -- gauge_set with labels
    ok, err = metrics.gauge_set("test.gauge.set.labels", 100, {
        queue = "emails"
    })
    assert.eq(ok, true, "gauge_set with labels works")
    assert.is_nil(err, "no error")

    -- gauge_inc
    ok, err = metrics.gauge_inc("test.gauge.inc")
    assert.eq(ok, true, "gauge_inc returns true")
    assert.is_nil(err, "gauge_inc no error")

    -- gauge_inc with labels
    ok, err = metrics.gauge_inc("test.gauge.inc.labels", {
        connection = "active"
    })
    assert.eq(ok, true, "gauge_inc with labels works")
    assert.is_nil(err, "no error")

    -- gauge_dec
    ok, err = metrics.gauge_dec("test.gauge.dec")
    assert.eq(ok, true, "gauge_dec returns true")
    assert.is_nil(err, "gauge_dec no error")

    -- gauge_dec with labels
    ok, err = metrics.gauge_dec("test.gauge.dec.labels", {
        connection = "active"
    })
    assert.eq(ok, true, "gauge_dec with labels works")
    assert.is_nil(err, "no error")

    return true
end

return { main = main }
