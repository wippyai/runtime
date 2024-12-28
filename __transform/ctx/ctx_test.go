package ctx

import (
	"context"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	lengine "github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestContextGetValue(t *testing.T) {
	cter := ctxapi.NewContexter[any]()
	cter.WithValue("testKey", "testValue")
	cter.WithValue("testKey2", 1)
	cter.WithValue("testKey3", map[string]string{"test": "test"})

	ctx := context.WithValue(context.Background(), ctxapi.ContexterKey, cter)
	log, _ := zap.NewDevelopment()

	engine := lengine.NewLuaEngine(ctx, log.Named("tests"))

	// Preload ctx module
	engine.L.PreloadModule("ctx", New[any](log.Named("ctx")).Loader)

	// Lua code to get value from context
	luaCode := `
	local ctx = require("ctx")
	local value, err = ctx.get("testKey")
	if err then
		return nil, err
	end
	return value
	`

	err := engine.DoString(luaCode, "TestContextGetValue")
	require.NoError(t, err)
	result := lengine.ToGoAny(engine.Get(-1))
	require.Equal(t, "testValue", result)

	luaCode2 := `
	local ctx = require("ctx")
	local value, err = ctx.get("testKey2")
	if err then
		return nil, err
	end
	return value
	`

	err = engine.DoString(luaCode2, "TestContextGetValue")
	require.NoError(t, err)
	result = lengine.ToGoAny(engine.Get(-1))
	require.Equal(t, float64(1), result)

	luaCode3 := `
	local ctx = require("ctx")
	local value, err = ctx.get("testKey3")
	if err then
		return nil, err
	end
	return value
	`

	err = engine.DoString(luaCode3, "TestContextGetValue")
	require.NoError(t, err)
	result = lengine.ToGoAny(engine.Get(-1))
	require.Equal(t, map[string]any{"test": "test"}, result)
	engine.Close()
}
