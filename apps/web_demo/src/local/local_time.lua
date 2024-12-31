local time = require("time")

function get_time()
    return { time = time.now():format(time.RFC3339) }
end
