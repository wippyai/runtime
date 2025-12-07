local logger = require("logger")
local queue = require("queue")
local store = require("store")

local function main(body)
    -- Get message metadata
    local msg, err = queue.message()
    if err then
        logger:error("failed to get message", {error = tostring(err)})
        return false
    end

    local msg_id = msg:id()
    local correlation_id = msg:header("correlation_id")

    logger:info("processing task", {
        msg_id = msg_id,
        correlation_id = correlation_id,
        body_type = type(body)
    })

    -- Get store instance
    local s, store_err = store.get("app.test.store:memory")
    if store_err then
        logger:error("failed to get store", {error = tostring(store_err)})
        return false
    end

    -- Store the processed message for verification
    local result_key = "queue:processed:" .. msg_id
    local result = {
        msg_id = msg_id,
        correlation_id = correlation_id,
        body = body,
        headers = msg:headers(),
        processed_at = os.time()
    }

    local ok, err = s:set(result_key, result, 300)
    if err then
        logger:error("failed to store result", {error = tostring(err)})
        return false
    end

    -- Increment counter for total processed messages
    local counter_key = "queue:counter"
    local current, _ = s:get(counter_key)
    local count = (current or 0) + 1
    s:set(counter_key, count, 300)

    logger:info("task processed successfully", {
        msg_id = msg_id,
        total_processed = count
    })

    return true
end

return { main = main }
