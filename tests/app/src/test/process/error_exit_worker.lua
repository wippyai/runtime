-- Worker that exits with error - should trigger LINK_DOWN
local function main()
    error("intentional error for testing")
end

return { main = main }
