package http

import (
	"context"
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestHttpContextConstants(t *testing.T) {
	logger := zap.NewNop()

	t.Run("verify METHOD constants", func(t *testing.T) {
		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			-- Verify all METHOD constants
			assert(http.METHOD.GET == "GET", "incorrect GET method")
			assert(http.METHOD.POST == "POST", "incorrect POST method")
			assert(http.METHOD.PUT == "PUT", "incorrect PUT method")
			assert(http.METHOD.DELETE == "DELETE", "incorrect DELETE method")
			assert(http.METHOD.PATCH == "PATCH", "incorrect PATCH method")
			assert(http.METHOD.HEAD == "HEAD", "incorrect HEAD method")
			assert(http.METHOD.OPTIONS == "OPTIONS", "incorrect OPTIONS method")
			
			-- Verify methods exist and have correct values
			assert(http.METHOD.GET == "GET", "incorrect GET method")
		`

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify STATUS constants", func(t *testing.T) {
		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			-- Verify all STATUS constants
			assert(http.STATUS.OK == 200, "incorrect OK status")
			assert(http.STATUS.CREATED == 201, "incorrect CREATED status")
			assert(http.STATUS.NO_CONTENT == 204, "incorrect NO_CONTENT status")
			assert(http.STATUS.BAD_REQUEST == 400, "incorrect BAD_REQUEST status")
			assert(http.STATUS.UNAUTHORIZED == 401, "incorrect UNAUTHORIZED status")
			assert(http.STATUS.NOT_FOUND == 404, "incorrect NOT_FOUND status")
			assert(http.STATUS.INTERNAL_ERROR == 500, "incorrect INTERNAL_ERROR status")
			
			-- Verify status codes exist and have correct values
			assert(http.STATUS.OK == 200, "incorrect OK status")
		`

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify CONTENT constants", func(t *testing.T) {
		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			-- Verify all CONTENT constants
			assert(http.CONTENT.JSON == "application/json", "incorrect JSON content type")
			assert(http.CONTENT.FORM == "application/x-www-form-urlencoded", "incorrect FORM content type")
			assert(http.CONTENT.MULTIPART == "multipart/form-data", "incorrect MULTIPART content type")
			assert(http.CONTENT.TEXT == "text/plain", "incorrect TEXT content type")
			assert(http.CONTENT.STREAM == "application/octet-stream", "incorrect STREAM content type")
			
			-- Verify content types exist and have correct values
			assert(http.CONTENT.JSON == "application/json", "incorrect JSON content type")
		`

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify TRANSFER constants", func(t *testing.T) {
		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			-- Verify all TRANSFER constants
			assert(http.TRANSFER.CHUNKED == "chunked", "incorrect CHUNKED transfer type")
			assert(http.TRANSFER.SSE == "sse", "incorrect SSE transfer type")
			
			-- Verify transfer types exist and have correct values
			assert(http.TRANSFER.CHUNKED == "chunked", "incorrect CHUNKED transfer type")
		`

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("verify ERROR constants", func(t *testing.T) {
		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			-- Verify all ERROR constants
			assert(http.ERROR.PARSE_FAILED == "PARSE_FAILED", "incorrect PARSE_FAILED error")
			assert(http.ERROR.INVALID_STATE == "INVALID_STATE", "incorrect INVALID_STATE error")
			assert(http.ERROR.WRITE_FAILED == "WRITE_FAILED", "incorrect WRITE_FAILED error")
			assert(http.ERROR.STREAM_ERROR == "STREAM_ERROR", "incorrect STREAM_ERROR error")
			
			-- Verify error types exist and have correct values
			assert(http.ERROR.PARSE_FAILED == "PARSE_FAILED", "incorrect PARSE_FAILED error")
		`

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}
