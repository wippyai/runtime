-- Micro test framework
-- Finds and runs all functions with meta.type = "test"
local io = require("io")
local registry = require("registry")
local funcs = require("funcs")

-- ANSI color helpers
local reset = "\027[0m"
local function bold(s) return "\027[1m" .. s .. reset end
local function red(s) return "\027[31m" .. s .. reset end
local function green(s) return "\027[32m" .. s .. reset end
local function yellow(s) return "\027[33m" .. s .. reset end
local function cyan(s) return "\027[36m" .. s .. reset end
local function dim(s) return "\027[2m" .. s .. reset end

-- Test result tracking
local results = {
    passed = 0,
    failed = 0,
    errors = {},
}

local function run_test(entry)
    local id = entry.id
    local name = entry.meta and entry.meta.comment or id

    io.write("  " .. dim("running ") .. cyan(id) .. " ")

    -- Use pcall to catch errors from the test function
    local ok, result, err = pcall(function()
        return funcs.call(id)
    end)

    if not ok then
        -- pcall caught a Lua error
        results.failed = results.failed + 1
        table.insert(results.errors, {
            id = id,
            error = tostring(result), -- result is the error message when ok=false
        })
        io.print(red("[FAIL]"))
        io.print("    " .. red(tostring(result)))
        return false
    end

    if err then
        -- funcs.call returned an error
        results.failed = results.failed + 1
        table.insert(results.errors, {
            id = id,
            error = tostring(err),
        })
        io.print(red("[FAIL]"))
        io.print("    " .. red(tostring(err)))
        return false
    end

    -- Check if result indicates success
    if result == false then
        results.failed = results.failed + 1
        table.insert(results.errors, {
            id = id,
            error = "test returned false",
        })
        io.print(red("[FAIL]"))
        return false
    end

    results.passed = results.passed + 1
    io.print(green("[PASS]"))
    return true
end

local function main()
    io.print("")
    io.print(bold(cyan("Wippy Test Runner")))
    io.print(dim("Finding tests with meta.type = 'test'..."))
    io.print("")

    -- Find all entries with meta.type = "test"
    -- Note: finder requires "meta." prefix for metadata fields
    local entries, err = registry.find({
        ["meta.type"] = "test"
    })

    if err then
        io.print(red("Failed to find tests: " .. tostring(err)))
        return 1
    end

    if not entries or #entries == 0 then
        io.print(yellow("No tests found."))
        io.print(dim("To create a test, add a function with meta.type: test in _index.yaml"))
        return 0
    end

    io.print("Found " .. bold(tostring(#entries)) .. " test(s)")
    io.print("")

    -- Run each test
    for _, entry in ipairs(entries) do
        run_test(entry)
    end

    -- Print summary
    io.print("")
    io.print(bold("Results:"))
    io.print("  " .. green(tostring(results.passed) .. " passed"))
    if results.failed > 0 then
        io.print("  " .. red(tostring(results.failed) .. " failed"))
    end
    io.print("")

    if results.failed > 0 then
        io.print(red("TESTS FAILED"))
        return 1
    end

    io.print(green("ALL TESTS PASSED"))
    return 0
end

return { main = main }
