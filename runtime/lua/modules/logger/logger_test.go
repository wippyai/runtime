package logger

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLoggerModule(t *testing.T) {
	t.Run("module creation and loading", func(t *testing.T) {
		core, _ := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local logger = require("logger")
			assert(type(logger) == "userdata")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("log levels with simple messages", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(core)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(nil, `
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

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(nil, `
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

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(nil, `
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

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(nil, `
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

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test empty name in named()
		err = vm.DoString(nil, `
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
		err = vm.DoString(nil, `
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
		err = vm.DoString(nil, `
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

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Clear any initialization logs
		logs.TakeAll()

		err = vm.DoString(nil, `
			local logger = require("logger")
			logger:error("operation failed", {
				err_msg = "database connection failed",  -- Changed from 'error' to 'err_msg'
				operation = "db_connect"
			})
		`, "test")
		require.NoError(t, err)

		entries := logs.All()
		require.Len(t, entries, 1)
		entry := entries[0]
		assert.Equal(t, "operation failed", entry.Message)
		assert.Equal(t, "database connection failed", entry.ContextMap()["err_msg"])
		assert.Equal(t, "db_connect", entry.ContextMap()["operation"])
	})
}
