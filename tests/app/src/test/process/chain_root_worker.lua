-- Chain root: spawns first link in chain, waits for all to be ready, then errors
local function main()
	local events_ch = process.events()
	local inbox_ch = process.inbox()

	local chain_depth = 5  -- Create 5-level chain

	-- Spawn first child in chain
	local first_child, err = process.spawn_monitored("app.test.process:chain_worker", "app:processes")
	if err then
		return false, "spawn first child failed: " .. tostring(err)
	end

	-- Send setup to first child
	process.send(first_child, "setup", {
		link_to = process.pid(),
		root_pid = process.pid(),
		depth = chain_depth,
	})

	-- Wait for all chain links to report ready (blocking)
	local ready_count = 0
	local expected = chain_depth + 1  -- depth+1 workers in total chain

	while ready_count < expected do
		local result = channel.select {
			inbox_ch:case_receive(),
			events_ch:case_receive(),
		}

		if result.channel == inbox_ch then
			local msg = result.value
			if msg and msg:topic() == "chain_ready" then
				ready_count = ready_count + 1
			end
		elseif result.channel == events_ch then
		-- Child crashed before all were ready
			return false, "child crashed before ready: " .. tostring(result.value.kind)
		end
	end

	-- All chain workers ready and linked - ERROR to trigger cascade
	error("CHAIN_ROOT_INTENTIONAL_ERROR")
end

return { main = main }
