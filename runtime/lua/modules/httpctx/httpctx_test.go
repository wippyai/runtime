package httpctx

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHttpContextConstants(t *testing.T) {
	logger := zap.NewNop()

	t.Run("verify METHOD constants", func(t *testing.T) {
		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			
			-- Verify all METHOD constants
			assert(httpctx.METHOD.GET == "GET", "incorrect GET method")
			assert(httpctx.METHOD.POST == "POST", "incorrect POST method")
			assert(httpctx.METHOD.PUT == "PUT", "incorrect PUT method")
			assert(httpctx.METHOD.DELETE == "DELETE", "incorrect DELETE method")
			assert(httpctx.METHOD.PATCH == "PATCH", "incorrect PATCH method")
			assert(httpctx.METHOD.HEAD == "HEAD", "incorrect HEAD method")
			assert(httpctx.METHOD.OPTIONS == "OPTIONS", "incorrect OPTIONS method")
			
			-- Verify methods exist and have correct values
			assert(httpctx.METHOD.GET == "GET", "incorrect GET method")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify STATUS constants", func(t *testing.T) {
		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			
			-- Verify all STATUS constants
			assert(httpctx.STATUS.OK == 200, "incorrect OK status")
			assert(httpctx.STATUS.CREATED == 201, "incorrect CREATED status")
			assert(httpctx.STATUS.NO_CONTENT == 204, "incorrect NO_CONTENT status")
			assert(httpctx.STATUS.BAD_REQUEST == 400, "incorrect BAD_REQUEST status")
			assert(httpctx.STATUS.UNAUTHORIZED == 401, "incorrect UNAUTHORIZED status")
			assert(httpctx.STATUS.NOT_FOUND == 404, "incorrect NOT_FOUND status")
			assert(httpctx.STATUS.INTERNAL_ERROR == 500, "incorrect INTERNAL_ERROR status")
			
			-- Verify status codes exist and have correct values
			assert(httpctx.STATUS.OK == 200, "incorrect OK status")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify CONTENT constants", func(t *testing.T) {
		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			
			-- Verify all CONTENT constants
			assert(httpctx.CONTENT.JSON == "application/json", "incorrect JSON content type")
			assert(httpctx.CONTENT.FORM == "application/x-www-form-urlencoded", "incorrect FORM content type")
			assert(httpctx.CONTENT.MULTIPART == "multipart/form-data", "incorrect MULTIPART content type")
			assert(httpctx.CONTENT.TEXT == "text/plain", "incorrect TEXT content type")
			assert(httpctx.CONTENT.STREAM == "application/octet-stream", "incorrect STREAM content type")
			
			-- Verify content types exist and have correct values
			assert(httpctx.CONTENT.JSON == "application/json", "incorrect JSON content type")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify TRANSFER constants", func(t *testing.T) {
		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			
			-- Verify all TRANSFER constants
			assert(httpctx.TRANSFER.CHUNKED == "chunked", "incorrect CHUNKED transfer type")
			assert(httpctx.TRANSFER.SSE == "sse", "incorrect SSE transfer type")
			
			-- Verify transfer types exist and have correct values
			assert(httpctx.TRANSFER.CHUNKED == "chunked", "incorrect CHUNKED transfer type")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify ERROR constants", func(t *testing.T) {
		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			
			-- Verify all ERROR constants
			assert(httpctx.ERROR.PARSE_FAILED == "PARSE_FAILED", "incorrect PARSE_FAILED error")
			assert(httpctx.ERROR.INVALID_STATE == "INVALID_STATE", "incorrect INVALID_STATE error")
			assert(httpctx.ERROR.WRITE_FAILED == "WRITE_FAILED", "incorrect WRITE_FAILED error")
			assert(httpctx.ERROR.STREAM_ERROR == "STREAM_ERROR", "incorrect STREAM_ERROR error")
			
			-- Verify error types exist and have correct values
			assert(httpctx.ERROR.PARSE_FAILED == "PARSE_FAILED", "incorrect PARSE_FAILED error")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})
}
