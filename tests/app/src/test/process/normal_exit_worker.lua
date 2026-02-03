-- Worker that exits normally (no error) - should NOT trigger LINK_DOWN
local function main()
	return true
end

return { main = main }
