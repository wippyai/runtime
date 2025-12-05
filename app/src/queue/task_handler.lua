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

    -- Store the processed message for verification
    local result_key = "queue:processed:" .. msg_id
    local result = {
        msg_id = msg_id,
        correlation_id = correlation_id,
        body = body,
        headers = msg:headers(),
        processed_at = os.time()
    }

    local ok, err = store.set(result_key, result, {ttl = 300})
    if err then
        logger:error("failed to store result", {error = tostring(err)})
        return false
    end

    -- Increment counter for total processed messages
    local counter_key = "queue:counter"
    local current, _ = store.get(counter_key)
    local count = (current or 0) + 1
    store.set(counter_key, count, {ttl = 300})

    logger:info("task processed successfully", {
        msg_id = msg_id,
        total_processed = count
    })

    return true
end

return { main = main }
