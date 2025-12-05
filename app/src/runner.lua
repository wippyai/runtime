-- Test runner
-- Finds and runs all functions with meta.type = "test"
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

-- Symbols
local SYM_PASS = green("✓")
local SYM_FAIL = red("✗")
local SYM_SUITE = "▸"

-- Results
local results = {
    passed = 0,
    failed = 0,
    errors = {},
}

-- Sort tests by order field (meta.order), then by id
local function sort_tests(tests)
    table.sort(tests, function(a, b)
        local order_a = (a.meta and a.meta.order) or 0
        local order_b = (b.meta and b.meta.order) or 0
        if order_a ~= order_b then
            return order_a < order_b
        end
        return a.id < b.id
    end)
    return tests
end

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

    -- Sort tests within each suite
    for _, tests in pairs(suites) do
        sort_tests(tests)
    end
    sort_tests(no_suite)

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

-- Extract short name from full id (e.g., "app.test.store:basic" -> "basic")
local function short_name(id)
    return id:match(":([^:]+)$") or id
end

-- Run single test, returns success and error message
local function run_test(entry)
    local ok, result, err = pcall(function()
        return funcs.call(entry.id)
    end)

    if not ok then
        return false, tostring(result)
    end
    if err then
        return false, tostring(err)
    end
    if result == false then
        return false, "returned false"
    end
    return true, nil
end

-- Run suite and collect results
local function run_suite(name, tests)
    local suite_passed = 0
    local suite_failed = 0
    local failures = {}

    for _, entry in ipairs(tests) do
        local ok, err_msg = run_test(entry)
        if ok then
            suite_passed = suite_passed + 1
            results.passed = results.passed + 1
        else
            suite_failed = suite_failed + 1
            results.failed = results.failed + 1
            table.insert(failures, {name = short_name(entry.id), error = err_msg})
            table.insert(results.errors, {id = entry.id, error = err_msg})
        end
    end

    -- Print suite line
    local status
    if suite_failed == 0 then
        status = green(suite_passed .. "/" .. #tests)
    else
        status = red(suite_failed .. " failed")
    end

    io.print("  " .. SYM_SUITE .. " " .. bold(name) .. " " .. dim("(" .. #tests .. ")") .. " " .. status)

    -- Print failures inline
    for _, f in ipairs(failures) do
        io.print("    " .. SYM_FAIL .. " " .. f.name .. dim(": ") .. red(f.error))
    end
end

local function main()
    io.print(bold(cyan("Tests")))

    -- Find tests
    local entries, err = registry.find({["meta.type"] = "test"})
    if err then
        io.print(red("Error: " .. tostring(err)))
        return 1
    end

    if not entries or #entries == 0 then
        io.print(yellow("No tests found"))
        return 0
    end

    -- Group by suite
    local suites, no_suite = group_by_suite(entries)
    local suite_names = sorted_keys(suites)

    io.print(dim(#entries .. " tests in " .. #suite_names .. " suites"))
    io.print("")

    -- Run suites
    for _, name in ipairs(suite_names) do
        run_suite(name, suites[name])
    end

    -- Run tests without suite
    if #no_suite > 0 then
        run_suite("other", no_suite)
    end

    -- Summary
    io.print("")
    if results.failed > 0 then
        io.print(red("FAILED") .. " " .. dim(results.passed .. " passed, " .. results.failed .. " failed"))
        return 1
    end

    io.print(green("PASSED") .. " " .. dim(results.passed .. " tests"))
    return 0
end

return { main = main }
