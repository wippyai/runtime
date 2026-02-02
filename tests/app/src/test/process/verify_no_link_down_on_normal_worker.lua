-- Tests that LINK_DOWN is NOT sent when parent exits normally
local function main()
-- Enable trap_links to be able to detect if LINK_DOWN is wrongly sent
	local ok, err = process.set_options({ trap_links = true })
	if not ok then
		return false, "set_options failed: " .. tostring(err)
	end

	local events_ch = process.events()

	-- Spawn linked child
	local _, err = process.spawn_linked_monitored("app.test.process:linked_child_worker", "app:processes")
	if err then
		return false, "spawn linked child failed: " .. tostring(err)
	end

	-- Spawn linked normal worker - when it exits normally, NO LINK_DOWN
	local _, err2 = process.spawn_linked_monitored("app.test.process:normal_exit_worker", "app:processes")
	if err2 then
		return false, "spawn normal worker failed: " .. tostring(err2)
	end

	-- Wait for EXIT event (we monitor the normal worker)
	local event = events_ch:receive()

	if event.kind == process.event.LINK_DOWN then
		return false, "got unexpected LINK_DOWN on normal exit"
	end

	if event.kind == process.event.EXIT then
		return true
	end

	return false, "unexpected event: " .. tostring(event.kind)
end

return { main = main }
