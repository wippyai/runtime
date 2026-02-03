local io = require("io")
local system = require("system")
local time = require("time")

local function format_bytes(bytes)
	if bytes < 1024 then
		return string.format("%d B", bytes)
	elseif bytes < 1024 * 1024 then
		return string.format("%.1f KB", bytes / 1024)
	elseif bytes < 1024 * 1024 * 1024 then
		return string.format("%.1f MB", bytes / (1024 * 1024))
	else
		return string.format("%.2f GB", bytes / (1024 * 1024 * 1024))
	end
end

local function main()
	local count = 10000
	io.print("Starting spawn test: " .. count .. " processes")

	-- Show initial memory
	local mem_before = system.memory.stats()
	io.print("Before: " .. format_bytes(mem_before.heap_alloc) .. " heap, " .. mem_before.heap_objects .. " objects")

	-- Spawn processes
	--
	--for i = 1, count do
	--    local pid, err = process.spawn("app.api:worker", "app:processes", "test " .. i)
	--    if pid then
	--        spawned = spawned + 1
	--    else
	--        errors = errors + 1
	--        if errors <= 5 then
	--            io.print("Spawn error: " .. tostring(err))
	--        end
	--    end
	--
	--    -- Progress every 1000
	--    if i % 1000 == 0 then
	--        io.print("Progress: " .. i .. "/" .. count)
	--    end
	--end
	--
	--local elapsed = os.clock() - start_time
	--io.print("")
	--io.print("Spawned: " .. spawned .. ", Errors: " .. errors)
	--io.print("Time: " .. string.format("%.2f", elapsed) .. "s")
	--
	---- Wait for processes to complete
	--io.print("Waiting 5s for processes to complete...")
	--os.execute("sleep 5")
	--
	---- Show memory after
	--local mem_after = system.memory.stats()
	--io.print("After: " .. format_bytes(mem_after.heap_alloc) .. " heap, " .. mem_after.heap_objects .. " objects")
	--
	---- Force GC via stats endpoint
	--io.print("")
	--io.print("Check http://localhost:6060/debug/gc for frame count")
	--io.print("Waiting 10 hours to keep process alive for profiling...")

	time.sleep("3600s")

	return 0
end

return { main = main }
