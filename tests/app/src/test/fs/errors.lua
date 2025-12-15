-- Test: Filesystem error handling with V2 structured errors
local assert = require("assert2")

local function main()
    local fs = require("fs")

    -- Getting non-existent filesystem
    local vol, err = fs.get("nonexistent")
    assert.is_nil(vol, "non-existent fs returns nil")
    assert.not_nil(err, "non-existent fs returns error")

    -- Get temp filesystem for testing
    vol, err = fs.get("app:temp")
    assert.not_nil(vol, "temp fs available")
    assert.is_nil(err, "temp fs no error")

    -- Stat non-existent file - returns V2 structured error
    local info, stat_err = vol:stat("/nonexistent_file_xyz")
    assert.is_nil(info, "stat non-existent returns nil")
    assert.not_nil(stat_err, "stat non-existent returns error")
    -- V2 structured error check
    assert.eq(stat_err:kind(), errors.NOT_FOUND, "stat error has NOT_FOUND kind")
    assert.not_nil(tostring(stat_err), "stat error has string representation")

    -- Open non-existent file for reading
    local file, open_err = vol:open("/nonexistent_file_xyz", "r")
    assert.is_nil(file, "open non-existent returns nil")
    assert.not_nil(open_err, "open non-existent returns error")
    assert.eq(open_err:kind(), errors.NOT_FOUND, "open error has NOT_FOUND kind")

    -- Invalid open mode - returns V2 structured error with INVALID kind
    file, open_err = vol:open("/test.txt", "invalid")
    assert.is_nil(file, "invalid mode returns nil")
    assert.not_nil(open_err, "invalid mode returns error")
    assert.eq(open_err:kind(), errors.INVALID, "invalid mode error has INVALID kind")

    -- Remove non-existent file
    local ok, rm_err = vol:remove("/nonexistent_file_xyz")
    assert.eq(ok, false, "remove non-existent returns false")
    assert.not_nil(rm_err, "remove non-existent returns error")

    -- Mkdir on existing path - returns ALREADY_EXISTS kind
    local test_dir = "/test_error_dir_" .. os.time()
    ok, err = vol:mkdir(test_dir)
    assert.ok(ok, "mkdir succeeds first time")

    ok, err = vol:mkdir(test_dir)
    assert.eq(ok, false, "mkdir on existing returns false")
    assert.not_nil(err, "mkdir on existing returns error")
    assert.eq(err:kind(), errors.ALREADY_EXISTS, "mkdir on existing has ALREADY_EXISTS kind")

    -- Cleanup
    vol:remove(test_dir)

    -- Chdir to non-existent directory
    ok, err = vol:chdir("/nonexistent_dir_xyz")
    assert.eq(ok, false, "chdir non-existent returns false")
    assert.not_nil(err, "chdir non-existent returns error")

    -- Chdir to file (not directory) - returns INVALID kind
    local test_file = "/test_error_file_" .. os.time() .. ".txt"
    ok, err = vol:writefile(test_file, "test")
    assert.ok(ok, "create test file")

    ok, err = vol:chdir(test_file)
    assert.eq(ok, false, "chdir to file returns false")
    assert.not_nil(err, "chdir to file returns error")
    assert.eq(err:kind(), errors.INVALID, "chdir to file has INVALID kind")

    vol:remove(test_file)

    -- Readdir on file (not directory) - returns INVALID kind
    test_file = "/test_error_file2_" .. os.time() .. ".txt"
    ok, err = vol:writefile(test_file, "test")
    assert.ok(ok, "create test file 2")

    local iter, rd_err = vol:readdir(test_file)
    assert.is_nil(iter, "readdir on file returns nil")
    assert.not_nil(rd_err, "readdir on file returns error")
    assert.eq(rd_err:kind(), errors.INVALID, "readdir on file has INVALID kind")

    vol:remove(test_file)

    -- Remove non-empty directory - returns INVALID kind
    test_dir = "/test_nonempty_" .. os.time()
    ok, err = vol:mkdir(test_dir)
    assert.ok(ok, "mkdir for non-empty test")

    ok, err = vol:writefile(test_dir .. "/file.txt", "content")
    assert.ok(ok, "create file in dir")

    ok, err = vol:remove(test_dir)
    assert.eq(ok, false, "remove non-empty returns false")
    assert.not_nil(err, "remove non-empty returns error")
    assert.eq(err:kind(), errors.INVALID, "remove non-empty has INVALID kind")

    -- Cleanup
    vol:remove(test_dir .. "/file.txt")
    vol:remove(test_dir)

    return true
end

return { main = main }
