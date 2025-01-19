package time

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

func TestTimeModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("core functions", func(t *testing.T) {
		t.Run("now()", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			err = vm.DoString(nil, `
				local time = require("time")
				local t = time.now()
				assert(type(t) == "userdata")
				assert(t:hour() >= 0 and t:hour() <= 23)
				assert(t:minute() >= 0 and t:minute() <= 59)
				assert(t:second() >= 0 and t:second() <= 59)
			`, "test")
			assert.NoError(t, err)
		})

		t.Run("sleep()", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			t.Run("normal sleep", func(t *testing.T) {
				start := time.Now()
				err = vm.DoString(nil, `
					local time = require("time")
					local duration = time.parse_duration("300ms")
					time.sleep(duration)
				`, "test")
				elapsed := time.Since(start)

				assert.NoError(t, err)
				assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(200)) // Allow for small timing variations
			})

			t.Run("sleep with context cancellation", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())

				// Start long sleep in a goroutine
				done := make(chan struct{})
				go func() {
					defer close(done)
					err := vm.DoString(ctx, `
						local time = require("time")
						local duration = time.parse_duration("5s")
						local err = time.sleep(duration)
						if err then
							assert(err:find("context canceled") ~= nil, "Expected context canceled error")
						end
					`, "test")
					assert.Error(t, err)
					assert.ErrorContains(t, err, "context canceled")
				}()

				// Wait a bit then cancel
				time.Sleep(100 * time.Millisecond)
				cancel()

				// Wait for completion
				select {
				case <-done:
					// Test completed normally
				case <-time.After(time.Second):
					t.Fatal("Test didn't complete in time")
				}
			})
		})

		t.Run("date()", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			err = vm.DoString(nil, `
				local time = require("time")
				local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)
				assert(t:year() == 2024)
				assert(t:month() == 12)
				assert(t:day() == 29)
				assert(t:hour() == 15)
				assert(t:minute() == 4)
				assert(t:second() == 5)
				assert(t:nanosecond() == 0)
			`, "test")
			assert.NoError(t, err)
		})

		t.Run("unix()", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			err = vm.DoString(nil, `
				local time = require("time")
				local t = time.unix(1735484645, 0)  -- 2024-12-29 15:04:05 UTC
				local utc_t = t:utc() -- Convert to UTC
				
				assert(utc_t:year() == 2024, "Expected year 2024, got " .. utc_t:year())
				assert(utc_t:month() == 12, "Expected month 12, got " .. utc_t:month())
				assert(utc_t:day() == 29, "Expected day 29, got " .. utc_t:day())
				assert(utc_t:hour() == 15, "Expected hour 15, got " .. utc_t:hour())
				assert(utc_t:minute() == 4, "Expected minute 4, got " .. utc_t:minute())
				assert(utc_t:second() == 5, "Expected second 5, got " .. utc_t:second())
			`, "test")
			assert.NoError(t, err)
		})

		t.Run("parse()", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			err = vm.DoString(nil, `
				local time = require("time")
				local t = time.parse("2006-01-02 15:04:05", "2024-12-29 15:04:05")
				assert(t:year() == 2024)
				assert(t:month() == 12)
				assert(t:day() == 29)
				assert(t:hour() == 15)
				assert(t:minute() == 4)
				assert(t:second() == 5)

				-- Test error case
				local bad_t, err = time.parse("2006-01-02", "invalid-date")
				assert(bad_t == nil)
				assert(type(err) == "string")
			`, "test")
			assert.NoError(t, err)
		})
	})

	t.Run("time methods - add and sub", func(t *testing.T) {
		t.Run("add()", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			err = vm.DoString(nil, `
				local time = require("time")
				local t = time.date(2024, 12, 29, 15, 0, 0, 0, time.utc)
				local duration = time.parse_duration("1h")
				local new_t = t:add(duration)
				assert(new_t:hour() == 16)
				assert(new_t:minute() == 0)
			`, "test")
			assert.NoError(t, err)
		})

		t.Run("sub() with time", func(t *testing.T) {
			mod := NewTimeModule()
			vm, err := engine.NewVM(logger,
				engine.WithLoader(mod.Name(), mod.Loader),
				engine.WithGlobalFunction("assert", assertLua),
			)
			require.NoError(t, err)
			defer vm.Close()

			err = vm.DoString(nil, `
				local time = require("time")
				local t1 = time.date(2024, 12, 29, 15, 0, 0, 0, time.utc)
				local t2 = time.date(2024, 12, 29, 14, 0, 0, 0, time.utc)
				local duration = t1:sub(t2)
				assert(duration:hours() == 1)
			`, "test")
			assert.NoError(t, err)
		})
	})
}

func TestTimeModule_TestBath(t *testing.T) {
	logger := zap.NewNop()

	t.Run("time methods", func(t *testing.T) {
		testCases := []struct {
			name     string
			script   string
			expected interface{}
		}{
			{
				name: "add_date",
				script: `
                    local time = require("time")
                    local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
                    local new_t = t:add_date(1, 2, 3)
                    return new_t:format("2006-01-02")
                `,
				expected: "2025-03-04",
			},
			{
				name: "format",
				script: `
                    local time = require("time")
                    local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)
                    return t:format("Mon Jan 2 15:04:05 MST 2006")
                `,
				expected: "Sun Dec 29 15:04:05 UTC 2024",
			},
			{
				name: "weekday",
				script: `
                    local time = require("time")
                    local t = time.date(2024, 12, 29, 0, 0, 0, 0, time.utc)
                    return t:weekday()
                `,
				expected: "0",
			},
			{
				name: "weekday",
				script: `
                    local time = require("time")
                    local t = time.date(2024, 12, 30, 0, 0, 0, 0, time.utc)
                    return t:weekday()
                `,
				expected: "1",
			},
			{
				name: "comparison_methods",
				script: `
                    local time = require("time")
                    local t1 = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
                    local t2 = time.date(2024, 1, 2, 0, 0, 0, 0, time.utc)
                    return t1:before(t2), t2:after(t1), t1:equal(t1)
                `,
				expected: []bool{true, true, true},
			},
			// Add more test cases for other methods
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.script, "test")
				require.NoError(t, err)

				// Handle multiple return values if needed
				switch expected := tc.expected.(type) {
				case []bool:
					for i := range expected {
						result := vm.State().Get(-len(expected) + i)
						assert.Equal(t, expected[i], bool(result.(lua.LBool)))
					}
					vm.State().Pop(len(expected))
				default:
					result := vm.State().Get(-1)
					assert.Equal(t, tc.expected, result.String())
					vm.State().Pop(1)
				}
			})
		}
	})

	t.Run("time methods", func(t *testing.T) {
		testCases := []struct {
			name     string
			script   string
			expected interface{}
		}{
			{
				name: "year_day",
				script: `
					local time = require("time")
					local t = time.date(2024, 12, 29, 0, 0, 0, 0, time.utc)
					return t:year_day()
				`,
				expected: "364",
			},
			{
				name: "year_day for leap year",
				script: `
					local time = require("time")
					local t = time.date(2024, 12, 31, 0, 0, 0, 0, time.utc)
					return t:year_day()
				`,
				expected: "366",
			},
			{
				name: "is_zero",
				script: `
					local time = require("time")
					local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
					return t:is_zero()
				`,
				expected: false,
			},
			{
				name: "is_zero for zero time",
				script: `
					local time = require("time")
					local t = time.unix(0, 0)
					return t:is_zero()
				`,
				expected: false, // Go's time.IsZero() returns false for Unix epoch
			},
			{
				name: "in location",
				script: `
					local time = require("time")
					local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
					assert(t ~= nil)
					local loc = time.load_location("America/New_York")
                    
					local new_t = t:in_location(loc)
					return new_t:format("2006-01-02 15:04:05 MST")
				`,
				expected: "2023-12-31 19:00:00 EST",
			},
			{
				name: "location",
				script: `
					local time = require("time")
					local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
					local loc = t:location()
					return tostring(loc)
				`,
				expected: "UTC",
			},
			{
				name: "location after in()",
				script: `
					local time = require("time")
					local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
					local loc = time.load_location("America/New_York")
					local new_t = t:in_location(loc)
					local new_loc = new_t:location()
					return tostring(new_loc)
				`,
				expected: "America/New_York",
			},
			{
				name: "utc",
				script: `
					local time = require("time")
					local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.load_location("America/New_York"))
					local new_t = t:utc()
					return new_t:format("2006-01-02 15:04:05 MST")
				`,
				expected: "2024-01-01 05:00:00 UTC",
			},
			{
				name: "round",
				script: `
					local time = require("time")
					local t = time.parse("2006-01-02T15:04:05.999999999Z", "2024-01-01T12:34:56.789Z")
					local d = time.parse_duration("1s")
					local new_t = t:round(d)
					return new_t:format("2006-01-02T15:04:05Z")
				`,
				expected: "2024-01-01T12:34:57Z",
			},
			{
				name: "round down",
				script: `
					local time = require("time")
					local t = time.parse("2006-01-02T15:04:05.999999999Z", "2024-01-01T12:34:56.499Z")
					local d = time.parse_duration("1s")
					local new_t = t:round(d)
					return new_t:format("2006-01-02T15:04:05Z")
				`,
				expected: "2024-01-01T12:34:56Z",
			},
			{
				name: "truncate",
				script: `
					local time = require("time")
					local t = time.parse("2006-01-02T15:04:05.999999999Z", "2024-01-01T12:34:56.789Z")
					local d = time.parse_duration("1m")
					local new_t = t:truncate(d)
					return new_t:format("2006-01-02T15:04:05Z")
				`,
				expected: "2024-01-01T12:34:00Z",
			},
			{
				name: "unix",
				script: `
					local time = require("time")
					local t = time.date(1970, 1, 1, 0, 0, 1, 0, time.utc)
					return t:unix()
				`,
				expected: "1",
			},
			{
				name: "unix_nano",
				script: `
					local time = require("time")
					local t = time.date(1970, 1, 1, 0, 0, 0, 1, time.utc)
					return t:unix_nano()
				`,
				expected: "1",
			},
			{
				name: "date components",
				script: `
					local time = require("time")
					local t = time.date(2024, 12, 29, 1, 2, 3, 4, time.utc)
					local year, month, day = t:date()
					local hour, min, sec = t:clock()
					return year, month, day, hour, min, sec, t:nanosecond()
				`,
				expected: []string{"2024", "12", "29", "1", "2", "3", "4"},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.script, "test")
				require.NoError(t, err)

				// Handle multiple return values if needed
				switch expected := tc.expected.(type) {
				case []string:
					for i := range expected {
						result := vm.State().Get(-len(expected) + i)
						assert.Equal(t, expected[i], result.String())
					}
					vm.State().Pop(len(expected))
				case []bool:
					for i := range expected {
						result := vm.State().Get(-len(expected) + i)
						assert.Equal(t, expected[i], bool(result.(lua.LBool)))
					}
					vm.State().Pop(len(expected))
				case bool:
					result := vm.State().Get(-1)
					assert.Equal(t, expected, bool(result.(lua.LBool)))
					vm.State().Pop(1)
				default:
					result := vm.State().Get(-1)
					assert.Equal(t, tc.expected, result.String())
					vm.State().Pop(1)
				}
			})
		}
	})
	t.Run("error handling", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			expectedError string
		}{
			{
				name: "add invalid duration",
				script: `
                    local time = require("time")
                    local t = time.now()
                    local success, err = pcall(function() return t:add(123) end)
					return success, err
                `,
				expectedError: "duration expected",
			},
			{
				name: "sub invalid time",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:sub(123) end)
					return success, err
                `,
				expectedError: "time expected",
			},
			{
				name: "add_date invalid arguments",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:add_date("invalid", 2, 3) end)
					return success, err
                `,
				expectedError: "number expected, got string",
			},
			{
				name: "after invalid time",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:after(123) end)
					return success, err
                `,
				expectedError: "time expected",
			},
			{
				name: "before invalid time",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:before(123) end)
					return success, err
                `,
				expectedError: "time expected",
			},
			{
				name: "equal invalid time",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:equal(123) end)
					return success, err
                `,
				expectedError: "time expected",
			},
			{
				name: "in invalid location",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:in_location(123) end)
					return success, err
                `,
				expectedError: "location expected",
			},
			{
				name: "round invalid duration",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:round(123) end)
					return success, err
                `,
				expectedError: "duration expected",
			},
			{
				name: "truncate invalid duration",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:truncate(123) end)
					return success, err
                `,
				expectedError: "duration expected",
			},
			{
				name: "call invalid method",
				script: `
                    local time = require("time")
                    local t = time.now()
					local success, err = pcall(function() return t:invalid_method() end)
					return success, err
                `,
				expectedError: "attempt to call a non-function object",
			},
		}
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.script, "test")
				require.NoError(t, err)

				success := vm.State().Get(-2).(lua.LBool)
				assert.False(t, bool(success))
				errStr := vm.State().Get(-1).String()
				assert.Contains(t, errStr, tc.expectedError)
				vm.State().Pop(2)
			})
		}
	})
}

func TestSleep(t *testing.T) {
	logger := zap.NewNop()

	t.Run("sleep function", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			minDuration   time.Duration
			expectError   bool
			errorContains string
		}{
			{
				name: "sleep with duration object",
				script: `
					local time = require("time")
					local duration = time.parse_duration("100ms")
					time.sleep(duration)
				`,
				minDuration: 50 * time.Millisecond,
			},
			{
				name: "sleep with string",
				script: `
					local time = require("time")
					time.sleep("100ms")
				`,
				minDuration: 50 * time.Millisecond,
			},
			{
				name: "sleep with invalid string",
				script: `
					local time = require("time")
					local err = time.sleep("invalid")
					assert(err ~= nil)
					return err
				`,
				expectError:   true,
				errorContains: "time: invalid duration",
			},
			{
				name: "sleep with invalid type",
				script: `
					local time = require("time")
					local success, err = pcall(function()
						time.sleep(123)
					end)
					return success, err
				`,
				expectError:   true,
				errorContains: "duration or string expected",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				start := time.Now()
				err = vm.DoString(nil, tc.script, "test")

				if tc.expectError {
					if tc.errorContains == "time: invalid duration" {
						assert.NoError(t, err)
						errStr := vm.State().Get(-1).String()
						assert.Contains(t, errStr, tc.errorContains)
						vm.State().Pop(1)
					} else {
						assert.NoError(t, err)
						success := vm.State().Get(-2).(lua.LBool)
						assert.False(t, bool(success))
						errStr := vm.State().Get(-1).String()
						assert.Contains(t, errStr, tc.errorContains)
						vm.State().Pop(2)
					}
				} else {
					assert.NoError(t, err)
					elapsed := time.Since(start)
					assert.GreaterOrEqual(t, elapsed, tc.minDuration,
						"Sleep duration was shorter than expected")
				}
			})
		}
	})

	t.Run("sleep with context cancellation", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "cancel sleep with duration object",
				script: `
					local time = require("time")
					local duration = time.parse_duration("5s")
					local err = time.sleep(duration)
					assert(err:find("context canceled") ~= nil, "Expected context canceled error")
				`,
			},
			{
				name: "cancel sleep with string duration",
				script: `
					local time = require("time")
					local err = time.sleep("5s")
					assert(err:find("context canceled") ~= nil, "Expected context canceled error")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				ctx, cancel := context.WithCancel(context.Background())

				// Start long sleep in a goroutine
				done := make(chan struct{})
				go func() {
					defer close(done)
					err := vm.DoString(ctx, tc.script, "test")
					assert.Error(t, err)
					assert.ErrorContains(t, err, "context canceled")
				}()

				// Wait a bit then cancel
				time.Sleep(100 * time.Millisecond)
				cancel()

				// Wait for completion
				select {
				case <-done:
					// Test completed normally
				case <-time.After(time.Second):
					t.Fatal("Test didn't complete in time")
				}
			})
		}
	})
}
