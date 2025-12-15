// Package boot provides application boot and component loading.
package boot

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

func TestConfig_Get(t *testing.T) {
	cfg := NewConfig(
		WithSection("test", map[string]any{
			"key1": "value1",
			"key2": 42,
		}),
	)

	t.Run("existing key", func(t *testing.T) {
		v, ok := cfg.Get("test.key1")
		if !ok {
			t.Fatal("expected key to exist")
		}
		if v != "value1" {
			t.Errorf("expected value1, got %v", v)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, ok := cfg.Get("test.missing")
		if ok {
			t.Fatal("expected key to not exist")
		}
	})
}

func TestConfig_GetString(t *testing.T) {
	cfg := NewConfig(
		WithSection("test", map[string]any{
			"str":   "hello",
			"int":   42,
			"empty": "",
		}),
	)

	t.Run("existing string", func(t *testing.T) {
		v := cfg.GetString("test.str", "default")
		if v != "hello" {
			t.Errorf("expected hello, got %s", v)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		v := cfg.GetString("test.empty", "default")
		if v != "" {
			t.Errorf("expected empty string, got %s", v)
		}
	})

	t.Run("missing key returns default", func(t *testing.T) {
		v := cfg.GetString("test.missing", "default")
		if v != "default" {
			t.Errorf("expected default, got %s", v)
		}
	})

	t.Run("type mismatch returns default", func(t *testing.T) {
		v := cfg.GetString("test.int", "default")
		if v != "default" {
			t.Errorf("expected default, got %s", v)
		}
	})
}

func TestConfig_GetInt(t *testing.T) {
	cfg := NewConfig(
		WithSection("test", map[string]any{
			"int": 42,
			"str": "hello",
		}),
	)

	t.Run("existing int", func(t *testing.T) {
		v := cfg.GetInt("test.int", 0)
		if v != 42 {
			t.Errorf("expected 42, got %d", v)
		}
	})

	t.Run("missing key returns default", func(t *testing.T) {
		v := cfg.GetInt("test.missing", 99)
		if v != 99 {
			t.Errorf("expected 99, got %d", v)
		}
	})

	t.Run("type mismatch returns default", func(t *testing.T) {
		v := cfg.GetInt("test.str", 99)
		if v != 99 {
			t.Errorf("expected 99, got %d", v)
		}
	})
}

func TestConfig_GetBool(t *testing.T) {
	cfg := NewConfig(
		WithSection("test", map[string]any{
			"bool": true,
			"str":  "hello",
		}),
	)

	t.Run("existing bool", func(t *testing.T) {
		v := cfg.GetBool("test.bool", false)
		if !v {
			t.Error("expected true")
		}
	})

	t.Run("missing key returns default", func(t *testing.T) {
		v := cfg.GetBool("test.missing", false)
		if v {
			t.Error("expected false")
		}
	})

	t.Run("type mismatch returns default", func(t *testing.T) {
		v := cfg.GetBool("test.str", false)
		if v {
			t.Error("expected false")
		}
	})
}

func TestConfig_GetDuration(t *testing.T) {
	cfg := NewConfig(
		WithSection("test", map[string]any{
			"duration": 5 * time.Second,
			"str":      "hello",
		}),
	)

	t.Run("existing duration", func(t *testing.T) {
		v := cfg.GetDuration("test.duration", time.Minute)
		if v != 5*time.Second {
			t.Errorf("expected 5s, got %v", v)
		}
	})

	t.Run("missing key returns default", func(t *testing.T) {
		v := cfg.GetDuration("test.missing", time.Minute)
		if v != time.Minute {
			t.Errorf("expected 1m, got %v", v)
		}
	})

	t.Run("type mismatch returns default", func(t *testing.T) {
		v := cfg.GetDuration("test.str", time.Minute)
		if v != time.Minute {
			t.Errorf("expected 1m, got %v", v)
		}
	})
}

func TestConfig_Sub(t *testing.T) {
	cfg := NewConfig(
		WithSection("http", map[string]any{
			"port": 8080,
			"host": "localhost",
		}),
		WithSection("entry", map[string]any{
			"app:gateway.addr": ":9090",
			"app:gateway.tls":  true,
			"app:worker.count": 4,
		}),
		WithSection("database", map[string]any{
			"host":           "db.local",
			"connection.max": 10,
		}),
	)

	t.Run("single level sub", func(t *testing.T) {
		httpCfg := cfg.Sub("http")
		port := httpCfg.GetInt("port", 0)
		if port != 8080 {
			t.Errorf("expected 8080, got %d", port)
		}

		host := httpCfg.GetString("host", "")
		if host != "localhost" {
			t.Errorf("expected localhost, got %s", host)
		}
	})

	t.Run("chained sub", func(t *testing.T) {
		entryCfg := cfg.Sub("entry")
		gatewayCfg := entryCfg.Sub("app:gateway")

		addr := gatewayCfg.GetString("addr", "")
		if addr != ":9090" {
			t.Errorf("expected :9090, got %s", addr)
		}

		tls := gatewayCfg.GetBool("tls", false)
		if !tls {
			t.Error("expected true")
		}
	})

	t.Run("nested sub", func(t *testing.T) {
		dbConnCfg := cfg.Sub("database").Sub("connection")
		maxConn := dbConnCfg.GetInt("max", 0)
		if maxConn != 10 {
			t.Errorf("expected 10, got %d", maxConn)
		}
	})

	t.Run("sub with missing prefix", func(t *testing.T) {
		missingCfg := cfg.Sub("missing")
		v := missingCfg.GetString("key", "default")
		if v != "default" {
			t.Errorf("expected default, got %s", v)
		}
	})
}

func TestConfig_Keys(t *testing.T) {
	cfg := NewConfig(
		WithSection("http", map[string]any{
			"port": 8080,
			"host": "localhost",
		}),
		WithSection("database", map[string]any{
			"host": "db.local",
			"port": 5432,
		}),
		WithSection("entry", map[string]any{
			"app:gateway.addr": ":9090",
		}),
	)

	t.Run("root keys", func(t *testing.T) {
		keys := cfg.Keys()
		if len(keys) != 5 {
			t.Errorf("expected 5 keys, got %d", len(keys))
		}
	})

	t.Run("scoped keys", func(t *testing.T) {
		httpCfg := cfg.Sub("http")
		keys := httpCfg.Keys()
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}

		hasPort := false
		hasHost := false
		for _, k := range keys {
			if k == "port" {
				hasPort = true
			}
			if k == "host" {
				hasHost = true
			}
		}

		if !hasPort || !hasHost {
			t.Errorf("expected port and host keys, got %v", keys)
		}
	})

	t.Run("deeply scoped keys", func(t *testing.T) {
		entryCfg := cfg.Sub("entry").Sub("app:gateway")
		keys := entryCfg.Keys()
		if len(keys) != 1 {
			t.Errorf("expected 1 key, got %d", len(keys))
		}
		if keys[0] != "addr" {
			t.Errorf("expected addr key, got %s", keys[0])
		}
	})

	t.Run("empty scope keys", func(t *testing.T) {
		missingCfg := cfg.Sub("missing")
		keys := missingCfg.Keys()
		if len(keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(keys))
		}
	})
}

func TestWithConfig(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		cfg := NewConfig(WithSection("test", map[string]any{"key": "value"}))

		ctx = WithConfig(ctx, cfg)

		retrieved := GetConfig(ctx)
		assert.NotNil(t, retrieved)
		v, ok := retrieved.Get("test.key")
		assert.True(t, ok)
		assert.Equal(t, "value", v)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()
		cfg := NewConfig(WithSection("test", map[string]any{"key": "value"}))

		result := WithConfig(ctx, cfg)

		assert.Equal(t, ctx, result)
		assert.Nil(t, GetConfig(result))
	})

	t.Run("idempotent", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		cfg1 := NewConfig(WithSection("test", map[string]any{"key": "value1"}))
		cfg2 := NewConfig(WithSection("test", map[string]any{"key": "value2"}))

		ctx = WithConfig(ctx, cfg1)
		WithConfig(ctx, cfg2)

		retrieved := GetConfig(ctx)
		v, _ := retrieved.Get("test.key")
		assert.Equal(t, "value1", v)
	})
}

func TestGetConfig(t *testing.T) {
	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()
		cfg := GetConfig(ctx)
		assert.Nil(t, cfg)
	})

	t.Run("with app context but no config", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		cfg := GetConfig(ctx)
		assert.Nil(t, cfg)
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ac := ctxapi.AppFromContext(ctx)
		ac.With(configCtxKey, "not a config")

		cfg := GetConfig(ctx)
		assert.Nil(t, cfg)
	})
}

func TestWithLoader(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ldr := &mockLoader{}

		ctx = WithLoader(ctx, ldr)

		retrieved := GetLoader(ctx)
		assert.NotNil(t, retrieved)
		assert.Equal(t, ldr, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()
		ldr := &mockLoader{}

		result := WithLoader(ctx, ldr)

		assert.Equal(t, ctx, result)
		assert.Nil(t, GetLoader(result))
	})

	t.Run("idempotent", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ldr1 := &mockLoader{}
		ldr2 := &mockLoader{}

		ctx = WithLoader(ctx, ldr1)
		WithLoader(ctx, ldr2)

		retrieved := GetLoader(ctx)
		assert.Equal(t, ldr1, retrieved)
	})
}

func TestGetLoader(t *testing.T) {
	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()
		ldr := GetLoader(ctx)
		assert.Nil(t, ldr)
	})

	t.Run("with app context but no loader", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ldr := GetLoader(ctx)
		assert.Nil(t, ldr)
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ac := ctxapi.AppFromContext(ctx)
		ac.With(loaderKey{}, "not a loader")

		ldr := GetLoader(ctx)
		assert.Nil(t, ldr)
	})
}

func TestFuncComponent(t *testing.T) {
	t.Run("Name and DependsOn", func(t *testing.T) {
		comp := New(P{
			Name:      "test-component",
			DependsOn: []string{"dep1", "dep2"},
		})

		assert.Equal(t, "test-component", comp.Name())
		assert.Equal(t, []string{"dep1", "dep2"}, comp.DependsOn())
	})

	t.Run("Load with function", func(t *testing.T) {
		loadCalled := false
		comp := New(P{
			Name: "test",
			Load: func(ctx context.Context) (context.Context, error) {
				loadCalled = true
				return ctx, nil
			},
		})

		ctx := context.Background()
		_, err := comp.Load(ctx)
		assert.NoError(t, err)
		assert.True(t, loadCalled)
	})

	t.Run("Load without function", func(t *testing.T) {
		comp := New(P{Name: "test"})

		ctx := context.Background()
		result, err := comp.Load(ctx)
		assert.NoError(t, err)
		assert.Equal(t, ctx, result)
	})

	t.Run("Start with function", func(t *testing.T) {
		startCalled := false
		comp := New(P{
			Name: "test",
			Start: func(_ context.Context) error {
				startCalled = true
				return nil
			},
		})

		starter := comp.(Starter)
		err := starter.Start(context.Background())
		assert.NoError(t, err)
		assert.True(t, startCalled)
	})

	t.Run("Start without function", func(t *testing.T) {
		comp := New(P{Name: "test"})

		starter := comp.(Starter)
		err := starter.Start(context.Background())
		assert.NoError(t, err)
	})

	t.Run("Stop with function", func(t *testing.T) {
		stopCalled := false
		comp := New(P{
			Name: "test",
			Stop: func(_ context.Context) error {
				stopCalled = true
				return nil
			},
		})

		stopper := comp.(Stopper)
		err := stopper.Stop(context.Background())
		assert.NoError(t, err)
		assert.True(t, stopCalled)
	})

	t.Run("Stop without function", func(t *testing.T) {
		comp := New(P{Name: "test"})

		stopper := comp.(Stopper)
		err := stopper.Stop(context.Background())
		assert.NoError(t, err)
	})
}

type mockLoader struct{}

func (m *mockLoader) LoadFS(_ context.Context, _ fs.FS) ([]registry.Entry, error) {
	return nil, nil
}
func (m *mockLoader) LoadDir(_ context.Context, _ fs.FS, _ string) ([]registry.Entry, error) {
	return nil, nil
}
func (m *mockLoader) LoadFile(_ context.Context, _ fs.FS, _ string) ([]registry.Entry, error) {
	return nil, nil
}
