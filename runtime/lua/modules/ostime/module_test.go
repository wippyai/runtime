package ostime

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestOSTimeModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module registration", func(t *testing.T) {
		mod := NewOSTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			-- Verify os table exists
			assert(type(os) == "table", "os table not found")
			
			-- Verify functions exist
			assert(type(os.time) == "function", "not a function")
			assert(type(os.date) == "function", "not a function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("os.time", func(t *testing.T) {
		mod := NewOSTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		t.Run("returns current time", func(t *testing.T) {
			err = vm.DoString(context.Background(), `
				local now = os.time()
				assert(type(now) == "number")
				
				-- close to current time (within 2 seconds)
				local expected = os.time()
				assert(math.abs(now - expected) < 2)
			`, "test")
			assert.NoError(t, err)
		})

		t.Run("with table argument", func(t *testing.T) {
			err = vm.DoString(context.Background(), `
				local t = {
					year = 2023,
					month = 12,
					day = 25,
					hour = 12,
					min = 30,
					sec = 45
				}
				local timestamp = os.time(t)
				assert(type(timestamp) == "number")
				
				-- Use os.date to verify the timestamp
				local date = os.date("*t", timestamp)
				assert(date.year == 2023)
				assert(date.month == 12)
				assert(date.day == 25)
				assert(date.hour == 12)
				assert(date.min == 30)
				assert(date.sec == 45)
			`, "test")
			assert.NoError(t, err)
		})
	})

	t.Run("os.date", func(t *testing.T) {
		mod := NewOSTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		t.Run("string format", func(t *testing.T) {
			// Use a fixed timestamp for consistent testing
			// 2023-10-15 14:30:45 UTC
			fixedTime := time.Date(2023, 10, 15, 14, 30, 45, 0, time.UTC).Unix()

			err = vm.DoString(context.Background(), `
				local timestamp = ...
				
				-- Test various format specifiers
				local date = os.date("!%Y-%m-%d %H:%M:%S", timestamp)
				assert(date == "2023-10-15 14:30:45", "Got: " .. date)
				
				local day = os.date("!%d", timestamp)
				assert(day == "15", "Got: " .. day)
				
				local month = os.date("!%m", timestamp)
				assert(month == "10", "Got: " .. month)
				
				local year = os.date("!%Y", timestamp)
				assert(year == "2023", "Got: " .. year)
				
				local shortYear = os.date("!%y", timestamp)
				assert(shortYear == "23", "Got: " .. shortYear)
				
				local weekday = os.date("!%a", timestamp)
				assert(weekday == "Sun", "Got: " .. weekday)
				
				local fullWeekday = os.date("!%A", timestamp)
				assert(fullWeekday == "Sunday", "Got: " .. fullWeekday)
			`, "test", lua.LNumber(fixedTime))
			assert.NoError(t, err)
		})

		t.Run("table format", func(t *testing.T) {
			// Use a fixed timestamp for consistent testing
			// 2023-10-15 14:30:45 UTC
			fixedTime := time.Date(2023, 10, 15, 14, 30, 45, 0, time.UTC).Unix()

			err = vm.DoString(context.Background(), `
				local timestamp = ...
				
				-- Get date table
				local t = os.date("!*t", timestamp)
				assert(type(t) == "table")
				assert(t.year == 2023, "Year: " .. t.year)
				assert(t.month == 10, "Month: " .. t.month)
				assert(t.day == 15, "Day: " .. t.day)
				assert(t.hour == 14, "Hour: " .. t.hour)
				assert(t.min == 30, "Minute: " .. t.min)
				assert(t.sec == 45, "Second: " .. t.sec)
				assert(t.wday == 1, "Weekday: " .. t.wday) -- Sunday is 1 in Lua
				assert(t.yday >= 288, "Yearday: " .. t.yday) -- Not exact due to leap years
			`, "test", lua.LNumber(fixedTime))
			assert.NoError(t, err)
		})

		t.Run("format specifiers", func(t *testing.T) {
			// Use a fixed timestamp for consistent testing
			fixedTime := time.Date(2023, 10, 15, 14, 30, 45, 0, time.UTC).Unix()

			err = vm.DoString(context.Background(), `
				local timestamp = ...
				
				-- Test %c format (date and time)
				local c_format = os.date("!%c", timestamp)
				assert(type(c_format) == "string")
				
				-- Test %x format (date only)
				local x_format = os.date("!%x", timestamp)
				assert(type(x_format) == "string")
				
				-- Test %X format (time only)
				local X_format = os.date("!%X", timestamp)
				assert(type(X_format) == "string")
				assert(X_format == "14:30:45", "Got: " .. X_format)
			`, "test", lua.LNumber(fixedTime))
			assert.NoError(t, err)
		})
	})

	t.Run("os.clock", func(t *testing.T) {
		mod := NewOSTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
		-- Verify os.clock exists and returns a number
		local start = os.clock()
		assert(type(start) == "number", "os.clock should return a number")
		
		-- Sleep for a small amount of time
		local function sleep(n)
			local t0 = os.clock()
			while os.clock() - t0 <= n do end
		end
		
		sleep(0.1) -- Sleep for approximately 0.1 seconds
		
		-- Check that clock advanced
		local duration = os.clock() - start
		assert(duration > 0, "Clock didn't advance: " .. duration)
		assert(duration < 1, "Clock advanced too much: " .. duration)
	`, "test")
		assert.NoError(t, err)
	})
}
