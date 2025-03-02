local http = require("http_client")
local fs = require("fs")
local sql = require("sql")
local crypto = require("crypto")
local time = require("time")

local function define_tests()
    -- HTTP Module Tests
    describe("HTTP Client", function()
        it("should connect to application homepage", function()
            local response, err = http.get("http://localhost:8082/", {
                timeout = "5s"
            })
            expect(err).to_be_nil("HTTP request failed: " .. (err or "unknown error"))
            expect(response).not_to_be_nil("No response received")
            expect(response.status_code).to_equal(200, "Expected 200 status code")
        end)
    end)

    -- Database Module Tests
    describe("SQLite Database", function()
        it("should connect to system database", function()
            local db, err = sql.get("system:db")
            expect(err).to_be_nil("Failed to connect to database: " .. (err or "unknown error"))
            expect(db).not_to_be_nil("No database connection received")
        end)

        it("should verify database type", function()
            local db, err = sql.get("system:db")
            if err then error("Failed to connect to database: " .. err) end

            local dbType, err = db:type()
            expect(err).to_be_nil("Failed to get database type: " .. (err or "unknown error"))
            expect(dbType).to_equal(sql.type.sqlite, "Expected SQLite database")
            db:release()
        end)
    end)

    -- Crypto Module Tests
    describe("Crypto Module", function()
        it("should generate random bytes", function()
            local bytes, err = crypto.random.bytes(32)
            expect(err).to_be_nil("Failed to generate random bytes: " .. (err or "unknown error"))
            expect(bytes).not_to_be_nil("No random bytes generated")
            expect(#bytes).to_equal(32, "Expected 32 random bytes")
        end)

        it("should calculate HMAC", function()
            local hmac, err = crypto.hmac.sha256("test-key", "test-data")
            expect(err).to_be_nil("HMAC calculation failed: " .. (err or "unknown error"))
            expect(hmac).not_to_be_nil("No HMAC digest generated")
            expect(hmac).to_be_type("string", "HMAC digest should be a string")
        end)

        it("should encrypt and decrypt data", function()
            local key, err = crypto.random.bytes(32)
            local plaintext = "test-encryption-data"

            local encrypted, err = crypto.encrypt.aes(plaintext, key)
            expect(err).to_be_nil("Encryption failed: " .. (err or "unknown error"))
            expect(encrypted).not_to_be_nil("No encrypted data generated")

            local decrypted, err = crypto.decrypt.aes(encrypted, key)
            expect(err).to_be_nil("Decryption failed: " .. (err or "unknown error"))
            expect(decrypted).to_equal(plaintext, "Decryption did not match original plaintext")
        end)
    end)

    -- Time Module Tests
    describe("Time Module", function()
        it("should get current time", function()
            local now = time.now()
            expect(now).not_to_be_nil("Failed to get current time")
        end)

        it("should convert to unix timestamp", function()
            local now = time.now()
            local unix_ts = now:unix()
            expect(unix_ts).to_be_type("number", "Unix timestamp should be a number")
            expect(unix_ts > 0).to_be_true("Unix timestamp should be positive")
        end)

        it("should format date correctly", function()
            local now = time.now()
            local formatted = now:format(time.DateOnly)
            expect(formatted).to_be_type("string", "Formatted time should be a string")
            expect(formatted).to_match("^%d%d%d%d%-%d%d%-%d%d$", "Time format should match YYYY-MM-DD")
        end)

        it("should extract date components", function()
            local now = time.now()
            local year, month, day = now:date()
            expect(year).to_be_type("number", "Year should be a number")
            expect(month).to_be_type("number", "Month should be a number")
            expect(day).to_be_type("number", "Day should be a number")
        end)
    end)

    -- Filesystem Module Tests
    describe("Filesystem", function()
        it("should access system root filesystem", function()
            local root_fs = fs.get("system:root")
            expect(root_fs).not_to_be_nil("Failed to get root filesystem")
        end)

        it("should list directory contents", function()
            local root_fs = fs.get("system:root")
            local entries = {}
            for entry in root_fs:readdir("./") do
                table.insert(entries, entry.name)
            end
            expect(#entries > 0).to_be_true("Directory is empty or could not be read")
        end)

        it("should write and read files", function()
            local root_fs = fs.get("system:root")
            -- Use current directory with no "./" prefix and ensure a unique filename
            local tmp_file = "test_file_" .. os.time() .. ".txt"
            local test_content = "File write test at " .. os.date()

            local success, err = pcall(function()
                -- Write file
                local file = root_fs:open(tmp_file, "w")
                local write_ok = file:write(test_content)
                expect(write_ok).to_be_true("Failed to write to file")
                file:close()

                -- Read file
                local file = root_fs:open(tmp_file, "r")
                local content = file:read(1024) -- Specify size as a number
                file:close()

                expect(content).to_equal(test_content, "File content doesn't match what was written")

                -- Clean up
                root_fs:remove(tmp_file)
                local exists = root_fs:exists(tmp_file)
                expect(exists).to_be_false("Failed to delete temporary file")
            end)

            -- Cleanup in case of test failure
            if not success then
                if root_fs:exists(tmp_file) then
                    pcall(function() root_fs:remove(tmp_file) end)
                end
                error(err) -- Re-raise the error after cleanup
            end
        end)
    end)
end

return require("test").run_cases(define_tests)
