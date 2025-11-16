// Package boot provides application boot and component loading.
package boot

import (
	"testing"
	"time"
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
