package logger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLoggerModule(t *testing.T) {
	t.Run("module creation and loading", func(t *testing.T) {
		core, _ := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			assert(type(logger) == "userdata")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("log levels with simple messages", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			logger:debug("debug message")
			logger:info("info message")
			logger:warn("warning message")
			logger:error("error message")
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 4)
		assert.Equal(t, "debug message", entries[0].Message)
		assert.Equal(t, zap.DebugLevel, entries[0].Level)
		assert.Equal(t, "info message", entries[1].Message)
		assert.Equal(t, zap.InfoLevel, entries[1].Level)
		assert.Equal(t, "warning message", entries[2].Message)
		assert.Equal(t, zap.WarnLevel, entries[2].Level)
		assert.Equal(t, "error message", entries[3].Message)
		assert.Equal(t, zap.ErrorLevel, entries[3].Level)
	})

	t.Run("logging with fields", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			logger:info("user logged in", {
				user_id = 123,
				username = "testuser",
				is_admin = true
			})
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		entry := entries[0]
		assert.Equal(t, "user logged in", entry.Message)
		assert.Equal(t, float64(123), entry.ContextMap()["user_id"])
		assert.Equal(t, "testuser", entry.ContextMap()["username"])
		assert.Equal(t, true, entry.ContextMap()["is_admin"])
	})

	t.Run("logger:with() method", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			local contextLogger = logger:with({
				request_id = "req-123",
				service = "auth"
			})
			contextLogger:info("processing request")
			contextLogger:error("request failed")
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 2)
		for _, entry := range entries {
			assert.Equal(t, "req-123", entry.ContextMap()["request_id"])
			assert.Equal(t, "auth", entry.ContextMap()["service"])
		}
	})

	t.Run("logger:named() method", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			local authLogger = logger:named("auth")
			authLogger:info("initializing auth service")
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		assert.Contains(t, entries[0].LoggerName, "auth")
	})

	t.Run("error handling", func(t *testing.T) {
		core, _ := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test empty name in named()
		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			local success, err = pcall(function()
				logger:named("")
			end)
			assert(not success)
			return err
		`, "test")
		require.NoError(t, err)
		errMsg := vm.State().Get(-1).String()
		assert.Contains(t, errMsg, "name cannot be empty")
		vm.State().Pop(1)

		// Test invalid logger userdata
		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			local fake_logger = "not a logger"
			local success, err = pcall(function()
				fake_logger:info("test")
			end)
			assert(not success)
			return err
		`, "test")
		require.NoError(t, err)
		errMsg = vm.State().Get(-1).String()
		assert.Contains(t, errMsg, "attempt to call a non-function object")
		vm.State().Pop(1)

		// Test with() with non-table argument
		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			local success, err = pcall(function()
				logger:with("not a table")
			end)
			assert(not success)
			return err
		`, "test")
		require.NoError(t, err)
		errMsg = vm.State().Get(-1).String()
		assert.Contains(t, errMsg, "table expected")
		vm.State().Pop(1)
	})

	t.Run("error field handling", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local logger = require("logger")
			logger:error("operation failed", {
				error = "database connection failed",  -- Changed from 'error' to 'err_msg'
				operation = "db_connect"
			})
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		entry := entries[0]
		assert.Equal(t, "operation failed", entry.Message)
		assert.Equal(t, "database connection failed", entry.ContextMap()["error"])
		assert.Equal(t, "db_connect", entry.ContextMap()["operation"])
	})

	t.Run("error userdata logging", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		jsonMod := json.NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithLoader(jsonMod.Name(), jsonMod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local logger = require("logger")

			-- Create an error by decoding invalid JSON
			local result, err = json.decode("invalid json")

			-- Log the error with a custom field name
			logger:info("Processing failed", {
				parse_error = err,
				operation = "json_decode"
			})
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		entry := entries[0]
		assert.Equal(t, "Processing failed", entry.Message)

		// Verify the error field is present with the custom name
		assert.Contains(t, entry.ContextMap(), "parse_error")
		assert.NotEmpty(t, entry.ContextMap()["parse_error"])
		assert.Equal(t, "json_decode", entry.ContextMap()["operation"])
	})

	t.Run("multiple fields including error", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		jsonMod := json.NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithLoader(jsonMod.Name(), jsonMod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local logger = require("logger")

			local result, err = json.decode("bad json")

			-- Log multiple fields to verify no early return bug
			logger:warn("Multiple fields test", {
				field1 = "value1",
				error_field = err,
				field2 = "value2",
				field3 = 42
			})
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		entry := entries[0]

		// Verify ALL fields are present (no early return)
		assert.Equal(t, "value1", entry.ContextMap()["field1"])
		assert.Contains(t, entry.ContextMap(), "error_field")
		assert.Equal(t, "value2", entry.ContextMap()["field2"])
		assert.Equal(t, float64(42), entry.ContextMap()["field3"])
	})

	t.Run("nil error userdata", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := NewLoggerModule(logger)
		jsonMod := json.NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithLoader(jsonMod.Name(), jsonMod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		logs.TakeAll()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local logger = require("logger")

			-- Create success case (err will be nil)
			local result, err = json.decode('{"valid": "json"}')

			-- Log with nil error should not create empty error field
			logger:info("Success case", {
				result_error = err,
				status = "ok"
			})
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		entry := entries[0]

		// Verify nil error is not logged
		assert.NotContains(t, entry.ContextMap(), "result_error")
		assert.Equal(t, "ok", entry.ContextMap()["status"])
	})
}
