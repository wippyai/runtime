package runtimeconfig

import (
	"sync"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantNamespace string
		wantEntry     string
		wantField     string
		wantValue     string
		wantErr       bool
	}{
		{
			name:          "simple entry",
			input:         "app:gateway:addr=8080",
			wantNamespace: "app",
			wantEntry:     "gateway",
			wantField:     "addr",
			wantValue:     "8080",
			wantErr:       false,
		},
		{
			name:          "user name",
			input:         "app:user:name=John",
			wantNamespace: "app",
			wantEntry:     "user",
			wantField:     "name",
			wantValue:     "John",
			wantErr:       false,
		},
		{
			name:          "database host",
			input:         "app:db:host=localhost",
			wantNamespace: "app",
			wantEntry:     "db",
			wantField:     "host",
			wantValue:     "localhost",
			wantErr:       false,
		},
		{
			name:          "entry with dots",
			input:         "app:agents_by_name.endpoint:meta.router=core:api",
			wantNamespace: "app",
			wantEntry:     "agents_by_name.endpoint",
			wantField:     "meta.router",
			wantValue:     "core:api",
			wantErr:       false,
		},
		{
			name:          "value with equals sign",
			input:         "app:db:connstr=host=localhost;port=5432",
			wantNamespace: "app",
			wantEntry:     "db",
			wantField:     "connstr",
			wantValue:     "host=localhost;port=5432",
			wantErr:       false,
		},
		{
			name:          "value with spaces",
			input:         "app:message:content=Hello World",
			wantNamespace: "app",
			wantEntry:     "message",
			wantField:     "content",
			wantValue:     "Hello World",
			wantErr:       false,
		},
		{
			name:          "empty value",
			input:         "app:setting:value=",
			wantNamespace: "app",
			wantEntry:     "setting",
			wantField:     "value",
			wantValue:     "",
			wantErr:       false,
		},
		{
			name:          "single level field",
			input:         "config:timeout:duration=30",
			wantNamespace: "config",
			wantEntry:     "timeout",
			wantField:     "duration",
			wantValue:     "30",
			wantErr:       false,
		},
		{
			name:          "deeply nested field path",
			input:         "app:server:http.tls.cert.path=/etc/certs/server.crt",
			wantNamespace: "app",
			wantEntry:     "server",
			wantField:     "http.tls.cert.path",
			wantValue:     "/etc/certs/server.crt",
			wantErr:       false,
		},
		{
			name:    "missing first colon",
			input:   "appgateway:addr=8080",
			wantErr: true,
		},
		{
			name:    "missing second colon",
			input:   "app:gateway.addr=8080",
			wantErr: true,
		},
		{
			name:    "missing equals",
			input:   "app:gateway:addr",
			wantErr: true,
		},
		{
			name:    "empty namespace",
			input:   ":gateway:addr=8080",
			wantErr: true,
		},
		{
			name:    "empty entry",
			input:   "app::addr=8080",
			wantErr: true,
		},
		{
			name:    "empty field",
			input:   "app:gateway:=8080",
			wantErr: true,
		},
		{
			name:          "namespace with spaces trimmed",
			input:         " app :gateway:addr=8080",
			wantNamespace: "app",
			wantEntry:     "gateway",
			wantField:     "addr",
			wantValue:     "8080",
			wantErr:       false,
		},
		{
			name:          "entry and field with spaces trimmed",
			input:         "app: gateway : addr =8080",
			wantNamespace: "app",
			wantEntry:     "gateway",
			wantField:     "addr",
			wantValue:     "8080",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, entry, field, value, err := Parse(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Parse() unexpected error: %v", err)
				return
			}

			if namespace != tt.wantNamespace {
				t.Errorf("Parse() namespace = %q, want %q", namespace, tt.wantNamespace)
			}
			if entry != tt.wantEntry {
				t.Errorf("Parse() entry = %q, want %q", entry, tt.wantEntry)
			}
			if field != tt.wantField {
				t.Errorf("Parse() field = %q, want %q", field, tt.wantField)
			}
			if value != tt.wantValue {
				t.Errorf("Parse() value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

func TestConfigSet(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		entry     string
		field     string
		value     string
		wantErr   bool
	}{
		{
			name:      "simple set",
			namespace: "app",
			entry:     "port",
			field:     "value",
			value:     "8080",
			wantErr:   false,
		},
		{
			name:      "nested field set",
			namespace: "app",
			entry:     "gateway",
			field:     "addr",
			value:     "8080",
			wantErr:   false,
		},
		{
			name:      "deeply nested field set",
			namespace: "app",
			entry:     "server",
			field:     "http.tls.enabled",
			value:     "true",
			wantErr:   false,
		},
		{
			name:      "entry with dots",
			namespace: "app",
			entry:     "agents_by_name.endpoint",
			field:     "meta.router",
			value:     "core:api",
			wantErr:   false,
		},
		{
			name:      "empty field",
			namespace: "app",
			entry:     "port",
			field:     "",
			value:     "value",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			err := c.Set(tt.namespace, tt.entry, tt.field, tt.value)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Set() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Set() unexpected error: %v", err)
				return
			}

			// Verify the value was set
			val, exists, err := c.Get(tt.namespace, tt.entry, tt.field)
			if err != nil {
				t.Errorf("Get() unexpected error: %v", err)
				return
			}
			if !exists {
				t.Errorf("Get() value does not exist after Set()")
				return
			}
			if val != tt.value {
				t.Errorf("Get() = %q, want %q", val, tt.value)
			}
		})
	}
}

func TestConfigSetConflicts(t *testing.T) {
	t.Run("empty field name in path", func(t *testing.T) {
		c := New()
		err := c.Set("app", "gateway", "addr..port", "8080")
		if err == nil {
			t.Errorf("Set() expected error for empty field name in path")
		}
	})

	t.Run("can overwrite existing value", func(t *testing.T) {
		c := New()
		err := c.Set("app", "port", "value", "8080")
		if err != nil {
			t.Fatalf("First Set() failed: %v", err)
		}

		// Overwrite with new value
		err = c.Set("app", "port", "value", "9090")
		if err != nil {
			t.Errorf("Set() failed to overwrite existing value: %v", err)
		}

		val, exists, _ := c.GetString("app", "port", "value")
		if !exists || val != "9090" {
			t.Errorf("Get() = %q, want %q", val, "9090")
		}
	})
}

func TestConfigGet(t *testing.T) {
	c := New()
	// Setup test data
	_ = c.Set("app", "gateway", "addr", "8080")
	_ = c.Set("app", "user", "name", "John")
	_ = c.Set("app", "user", "age", "30")
	_ = c.Set("config", "timeout", "duration", "60")

	tests := []struct {
		name      string
		namespace string
		entry     string
		field     string
		wantValue string
		wantExist bool
	}{
		{
			name:      "existing simple field",
			namespace: "config",
			entry:     "timeout",
			field:     "duration",
			wantValue: "60",
			wantExist: true,
		},
		{
			name:      "existing nested field",
			namespace: "app",
			entry:     "gateway",
			field:     "addr",
			wantValue: "8080",
			wantExist: true,
		},
		{
			name:      "non-existent namespace",
			namespace: "nonexistent",
			entry:     "key",
			field:     "value",
			wantValue: "",
			wantExist: false,
		},
		{
			name:      "non-existent entry",
			namespace: "app",
			entry:     "nonexistent",
			field:     "path",
			wantValue: "",
			wantExist: false,
		},
		{
			name:      "non-existent field",
			namespace: "app",
			entry:     "gateway",
			field:     "nonexistent",
			wantValue: "",
			wantExist: false,
		},
		{
			name:      "entry map without field",
			namespace: "app",
			entry:     "user",
			field:     "",
			wantValue: "",
			wantExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, exists, err := c.Get(tt.namespace, tt.entry, tt.field)
			if err != nil {
				t.Errorf("Get() unexpected error: %v", err)
				return
			}

			if exists != tt.wantExist {
				t.Errorf("Get() exists = %v, want %v", exists, tt.wantExist)
			}

			if tt.wantExist && tt.wantValue != "" && val != tt.wantValue {
				t.Errorf("Get() value = %q, want %q", val, tt.wantValue)
			}
		})
	}
}

func TestConfigGetString(t *testing.T) {
	c := New()
	_ = c.Set("app", "name", "value", "TestApp")
	_ = c.Set("app", "version", "number", "1.0.0")

	t.Run("existing string", func(t *testing.T) {
		val, exists, err := c.GetString("app", "name", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if !exists {
			t.Errorf("GetString() value should exist")
		}
		if val != "TestApp" {
			t.Errorf("GetString() = %q, want %q", val, "TestApp")
		}
	})

	t.Run("non-existent key", func(t *testing.T) {
		_, exists, err := c.GetString("app", "nonexistent", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if exists {
			t.Errorf("GetString() value should not exist")
		}
	})
}

func TestConfigGetInt(t *testing.T) {
	c := New()
	_ = c.Set("app", "port", "value", "8080")
	_ = c.Set("app", "invalid", "value", "not-a-number")

	t.Run("valid integer", func(t *testing.T) {
		val, exists, err := c.GetString("app", "port", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if !exists {
			t.Errorf("GetString() value should exist")
		}
		if val != "8080" {
			t.Errorf("GetString() = %s, want %s", val, "8080")
		}
	})

	t.Run("invalid integer", func(t *testing.T) {
		val, exists, err := c.GetString("app", "invalid", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if !exists {
			t.Errorf("GetString() value should exist")
		}
		if val != "not-a-number" {
			t.Errorf("GetString() = %s, want %s", val, "not-a-number")
		}
	})

	t.Run("non-existent key", func(t *testing.T) {
		_, exists, err := c.GetString("app", "nonexistent", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if exists {
			t.Errorf("GetString() value should not exist")
		}
	})
}

func TestConfigGetBool(t *testing.T) {
	c := New()
	_ = c.Set("app", "enabled", "value", "true")
	_ = c.Set("app", "disabled", "value", "false")
	_ = c.Set("app", "numeric_true", "value", "1")
	_ = c.Set("app", "invalid", "value", "not-a-bool")

	tests := []struct {
		name      string
		entry     string
		wantValue string
		wantExist bool
	}{
		{
			name:      "true value",
			entry:     "enabled",
			wantValue: "true",
			wantExist: true,
		},
		{
			name:      "false value",
			entry:     "disabled",
			wantValue: "false",
			wantExist: true,
		},
		{
			name:      "numeric true",
			entry:     "numeric_true",
			wantValue: "1",
			wantExist: true,
		},
		{
			name:      "invalid bool",
			entry:     "invalid",
			wantValue: "not-a-bool",
			wantExist: true,
		},
		{
			name:      "non-existent",
			entry:     "nonexistent",
			wantValue: "",
			wantExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, exists, err := c.GetString("app", tt.entry, "value")

			if err != nil {
				t.Errorf("GetString() unexpected error: %v", err)
			}

			if exists != tt.wantExist {
				t.Errorf("GetString() exists = %v, want %v", exists, tt.wantExist)
			}

			if tt.wantExist && val != tt.wantValue {
				t.Errorf("GetString() = %v, want %v", val, tt.wantValue)
			}
		})
	}
}

func TestConfigGetFloat(t *testing.T) {
	c := New()
	_ = c.Set("app", "rate", "value", "3.14")
	_ = c.Set("app", "integer", "value", "42")
	_ = c.Set("app", "invalid", "value", "not-a-number")

	t.Run("valid float", func(t *testing.T) {
		val, exists, err := c.GetString("app", "rate", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if !exists {
			t.Errorf("GetString() value should exist")
		}
		if val != "3.14" {
			t.Errorf("GetString() = %s, want %s", val, "3.14")
		}
	})

	t.Run("integer as float", func(t *testing.T) {
		val, exists, err := c.GetString("app", "integer", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if !exists {
			t.Errorf("GetString() value should exist")
		}
		if val != "42" {
			t.Errorf("GetString() = %s, want %s", val, "42")
		}
	})

	t.Run("invalid float", func(t *testing.T) {
		val, exists, err := c.GetString("app", "invalid", "value")
		if err != nil {
			t.Errorf("GetString() unexpected error: %v", err)
		}
		if !exists {
			t.Errorf("GetString() value should exist")
		}
		if val != "not-a-number" {
			t.Errorf("GetString() = %s, want %s", val, "not-a-number")
		}
	})
}

func TestConfigHas(t *testing.T) {
	c := New()
	_ = c.Set("app", "gateway", "addr", "8080")

	tests := []struct {
		name      string
		namespace string
		entry     string
		field     string
		want      bool
	}{
		{
			name:      "existing key",
			namespace: "app",
			entry:     "gateway",
			field:     "addr",
			want:      true,
		},
		{
			name:      "non-existent entry",
			namespace: "app",
			entry:     "nonexistent",
			field:     "value",
			want:      false,
		},
		{
			name:      "non-existent namespace",
			namespace: "nonexistent",
			entry:     "key",
			field:     "value",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Has(tt.namespace, tt.entry, tt.field); got != tt.want {
				t.Errorf("Has() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetNamespace(t *testing.T) {
	c := New()
	_ = c.Set("app", "gateway", "addr", "8080")
	_ = c.Set("app", "user", "name", "John")
	_ = c.Set("config", "timeout", "duration", "60")

	t.Run("existing namespace", func(t *testing.T) {
		ns, exists := c.GetNamespace("app")
		if !exists {
			t.Errorf("GetNamespace() namespace should exist")
		}
		if ns == nil {
			t.Errorf("GetNamespace() should return non-nil map")
		}

		// Check structure
		if gateway, ok := ns["gateway"]; ok {
			if addr, ok := gateway["addr"]; !ok || addr != "8080" {
				t.Errorf("GetNamespace() incorrect nested structure")
			}
		} else {
			t.Errorf("GetNamespace() gateway should be a map")
		}
	})

	t.Run("non-existent namespace", func(t *testing.T) {
		_, exists := c.GetNamespace("nonexistent")
		if exists {
			t.Errorf("GetNamespace() namespace should not exist")
		}
	})
}

func TestConfigGetAllNamespaces(t *testing.T) {
	c := New()
	_ = c.Set("app", "key1", "value", "value1")
	_ = c.Set("config", "key2", "value", "value2")
	_ = c.Set("service", "key3", "value", "value3")

	namespaces := c.GetAllNamespaces()
	if len(namespaces) != 3 {
		t.Errorf("GetAllNamespaces() returned %d namespaces, want 3", len(namespaces))
	}

	expected := map[string]bool{"app": true, "config": true, "service": true}
	for _, ns := range namespaces {
		if !expected[ns] {
			t.Errorf("GetAllNamespaces() unexpected namespace: %s", ns)
		}
		delete(expected, ns)
	}

	if len(expected) > 0 {
		t.Errorf("GetAllNamespaces() missing namespaces: %v", expected)
	}
}

func TestConfigSetFromString(t *testing.T) {
	c := New()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid input",
			input:   "app:gateway:addr=8080",
			wantErr: false,
		},
		{
			name:    "entry with dots",
			input:   "app:agents_by_name.endpoint:meta.router=core:api",
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.SetFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigToMap(t *testing.T) {
	c := New()
	_ = c.Set("app", "gateway", "addr", "8080")
	_ = c.Set("app", "user", "name", "John")
	_ = c.Set("config", "timeout", "duration", "60")

	result := c.ToMap()

	if len(result) != 2 {
		t.Errorf("ToMap() returned %d namespaces, want 2", len(result))
	}

	if app, ok := result["app"]; ok {
		if gateway, ok := app["gateway"]; ok {
			if addr, ok := gateway["addr"]; !ok || addr != "8080" {
				t.Errorf("ToMap() incorrect nested value")
			}
		} else {
			t.Errorf("ToMap() gateway should be a map")
		}
	} else {
		t.Errorf("ToMap() app namespace should exist")
	}
}

func TestConfigConcurrency(_ *testing.T) {
	c := New()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			_ = c.Set("app", "key", "value", "test")
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			_, _, _ = c.Get("app", "key", "value")
		}(i)
	}

	wg.Wait()
}

func TestConfigComplexScenario(t *testing.T) {
	c := New()

	// Simulate the example usage from the requirement
	inputs := []string{
		"app:gateway:addr=8080",
		"app:user:name=John",
		"app:user:age=30",
		"app:db:host=localhost",
		"app:agents_by_name.endpoint:meta.router=core:api",
	}

	for _, input := range inputs {
		if err := c.SetFromString(input); err != nil {
			t.Fatalf("SetFromString(%q) failed: %v", input, err)
		}
	}

	// Verify all values
	t.Run("verify gateway addr", func(t *testing.T) {
		val, exists, _ := c.GetString("app", "gateway", "addr")
		if !exists || val != "8080" {
			t.Errorf("gateway.addr = %q, want %q", val, "8080")
		}
	})

	t.Run("verify user name", func(t *testing.T) {
		val, exists, _ := c.GetString("app", "user", "name")
		if !exists || val != "John" {
			t.Errorf("user.name = %q, want %q", val, "John")
		}
	})

	t.Run("verify user age as int", func(t *testing.T) {
		val, exists, err := c.GetString("app", "user", "age")
		if err != nil {
			t.Errorf("GetString() error: %v", err)
		}
		if !exists || val != "30" {
			t.Errorf("user.age = %s, want %s", val, "30")
		}
	})

	t.Run("verify db host", func(t *testing.T) {
		val, exists, _ := c.GetString("app", "db", "host")
		if !exists || val != "localhost" {
			t.Errorf("db.host = %q, want %q", val, "localhost")
		}
	})

	t.Run("verify entry with dots", func(t *testing.T) {
		val, exists, _ := c.GetString("app", "agents_by_name.endpoint", "meta.router")
		if !exists || val != "core:api" {
			t.Errorf("agents_by_name.endpoint.meta.router = %q, want %q", val, "core:api")
		}
	})

	t.Run("verify namespace structure", func(t *testing.T) {
		ns, exists := c.GetNamespace("app")
		if !exists {
			t.Fatal("app namespace should exist")
		}

		// Check that we have the expected top-level keys
		expectedKeys := []string{"gateway", "user", "db", "agents_by_name.endpoint"}
		for _, key := range expectedKeys {
			if _, ok := ns[key]; !ok {
				t.Errorf("namespace should contain key %q", key)
			}
		}
	})
}
