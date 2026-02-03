local logger = require("logger")
local time = require("time")

local ticks = input or 5

logger.info("Ticker process started", {input = input, ticks = ticks})

for i = 1, ticks do
	logger.info("Tick", {count = i, total = ticks})
	time.sleep("1s")
end

logger.info("Ticker process exiting normally")
return "completed " .. ticks .. " ticks"
