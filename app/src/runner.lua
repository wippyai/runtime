-- Test runner
-- Finds and runs all functions with meta.type = "test"
-- Displays test info from meta: suite, description
local io = require("io")
local registry = require("registry")
local funcs = require("funcs")

-- ANSI colors
local reset = "\027[0m"
local function bold(s) return "\027[1m" .. s .. reset end
local function red(s) return "\027[31m" .. s .. reset end
local function green(s) return "\027[32m" .. s .. reset end
local function yellow(s) return "\027[33m" .. s .. reset end
local function cyan(s) return "\027[36m" .. s .. reset end
local function dim(s) return "\027[2m" .. s .. reset end
local function magenta(s) return "\027[35m" .. s .. reset end

-- Results
local results = {
    passed = 0,
    failed = 0,
    errors = {},
}

-- Group tests by suite
local function group_by_suite(entries)
    local suites = {}
    local no_suite = {}

    for _, entry in ipairs(entries) do
        local suite = entry.meta and entry.meta.suite
        if suite then
            suites[suite] = suites[suite] or {}
            table.insert(suites[suite], entry)
        else
            table.insert(no_suite, entry)
        end
    end

    return suites, no_suite
end

-- Get sorted suite names
local function sorted_keys(t)
    local keys = {}
    for k in pairs(t) do
        table.insert(keys, k)
    end
    table.sort(keys)
    return keys
end

-- Run single test
local function run_test(entry)
    local id = entry.id
    local meta = entry.meta or {}
    local desc = meta.description or id

    io.write("    " .. dim(id) .. " ")

    local ok, result, err = pcall(function()
        return funcs.call(id)
    end)

    if not ok then
        results.failed = results.failed + 1
        table.insert(results.errors, {id = id, error = tostring(result)})
        io.print(red("FAIL"))
        io.print("      " .. red(tostring(result)))
        return false
    end

    if err then
        results.failed = results.failed + 1
        table.insert(results.errors, {id = id, error = tostring(err)})
        io.print(red("FAIL"))
        io.print("      " .. red(tostring(err)))
        return false
    end

    if result == false then
        results.failed = results.failed + 1
        table.insert(results.errors, {id = id, error = "returned false"})
        io.print(red("FAIL"))
        return false
    end

    results.passed = results.passed + 1
    io.print(green("OK"))
    return true
end

-- Print suite header
local function print_suite(name, tests)
    io.print("")
    io.print("  " .. bold(magenta(name)) .. dim(" (" .. #tests .. " tests)"))

    -- Show what the suite tests
    local first = tests[1]
    if first and first.meta and first.meta.description then
        -- Show first test's description as hint
    end
end

local function main()
    io.print("")
    io.print(bold(cyan("Test Runner")))
    io.print("")

    -- Find tests
    local entries, err = registry.find({["meta.type"] = "test"})
    if err then
        io.print(red("Error: " .. tostring(err)))
        return 1
    end

    if not entries or #entries == 0 then
        io.print(yellow("No tests found"))
        io.print(dim("Add meta.type: test to function entries"))
        return 0
    end

    -- Group by suite
    local suites, no_suite = group_by_suite(entries)
    local suite_names = sorted_keys(suites)

    -- Count
    local total = #entries
    io.print(dim("Found " .. total .. " tests"))

    -- Run suites
    for _, name in ipairs(suite_names) do
        local tests = suites[name]
        print_suite(name, tests)
        for _, entry in ipairs(tests) do
            run_test(entry)
        end
    end

    -- Run tests without suite
    if #no_suite > 0 then
        io.print("")
        io.print("  " .. bold(magenta("other")) .. dim(" (" .. #no_suite .. " tests)"))
        for _, entry in ipairs(no_suite) do
            run_test(entry)
        end
    end

    -- Summary
    io.print("")
    io.print(bold("Results"))
    io.print("  " .. green(results.passed .. " passed"))
    if results.failed > 0 then
        io.print("  " .. red(results.failed .. " failed"))
        io.print("")
        io.print(red("FAILED"))
        return 1
    end

    io.print("")
    io.print(green("PASSED"))
    return 0
end

return { main = main }
