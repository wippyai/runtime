-- Simple CLI app demonstrating terminal IO
local io = require("io")
local system = require("system")

-- ANSI color helpers
local reset = "\027[0m"
local function bold(s) return "\027[1m" .. s .. reset end
local function dim(s) return "\027[2m" .. s .. reset end
local function red(s) return "\027[31m" .. s .. reset end
local function green(s) return "\027[32m" .. s .. reset end
local function yellow(s) return "\027[33m" .. s .. reset end
local function cyan(s) return "\027[36m" .. s .. reset end
local function bright_cyan(s) return "\027[96m" .. s .. reset end
local function magenta(s) return "\027[35m" .. s .. reset end

-- Format bytes to human readable
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
    io.print(bold(cyan("Welcome to Wippy CLI!")))
    io.print("")

    -- Show system info
    local hostname = system.process.hostname()
    local cpus = system.runtime.cpu_count()
    local goroutines = system.runtime.goroutines()

    io.print(dim("System: ") .. magenta(hostname))
    io.print(dim("CPUs: ") .. bright_cyan(tostring(cpus)) .. dim("  Goroutines: ") .. bright_cyan(tostring(goroutines)))
    io.print("")

    io.write(yellow("Enter your name: "))
    local name = io.readline()

    if name and #name > 0 then
        io.print("Hello, " .. green(name) .. "!")
    else
        io.print("Hello, " .. dim("stranger") .. "!")
    end

    io.print("")
    io.write(yellow("Enter a number: "))
    local num = io.readline()

    local n = tonumber(num)
    if n then
        io.print("Your number squared: " .. bright_cyan(tostring(n * n)))
    else
        io.eprint(red("Invalid number: ") .. bold(tostring(num)))
        return 1
    end

    -- Show memory stats
    io.print("")
    local mem = system.memory.stats()
    io.print(dim("Memory: ") .. cyan(format_bytes(mem.heap_alloc)) .. dim(" allocated, ") .. cyan(format_bytes(mem.sys)) .. dim(" system"))

    io.print("")
    io.print(green("Goodbye!"))
    return 0
end

return { main = main }
