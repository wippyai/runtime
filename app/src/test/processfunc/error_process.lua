-- Process that returns error
local function main()
    error("intentional process error")
end

return { main = main }
