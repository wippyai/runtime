local assert = require("assert2")

local function main()
    local queue = require("queue")

    -- publish function exists
    assert.not_nil(queue.publish, "publish function should exist")

    -- publish requires queue ID
    local ok, err = queue.publish()
    assert.is_nil(ok, "publish without args should return nil")
    assert.not_nil(err, "publish without args should return error")

    -- publish requires message data
    ok, err = queue.publish("test:myqueue")
    assert.is_nil(ok, "publish without data should return nil")
    assert.not_nil(err, "publish without data should return error")

    return true
end

return { main = main }
