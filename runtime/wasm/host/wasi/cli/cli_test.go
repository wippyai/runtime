package cli

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

func TestEnvironmentHost(t *testing.T) {
	t.Run("creates with nil config", func(t *testing.T) {
		host := NewEnvironmentHost(nil)

		if host.env == nil {
			t.Fatal("expected non-nil environment")
		}
		if host.env.Cwd != "/" {
			t.Errorf("cwd = %s, want /", host.env.Cwd)
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		env := &Environment{
			Args: []string{"app", "--flag"},
			Env:  map[string]string{"HOME": "/home/user"},
			Cwd:  "/app",
		}
		host := NewEnvironmentHost(env)

		if len(host.env.Args) != 2 {
			t.Errorf("args count = %d, want 2", len(host.env.Args))
		}
		if host.env.Env["HOME"] != "/home/user" {
			t.Errorf("HOME = %s, want /home/user", host.env.Env["HOME"])
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		host := NewEnvironmentHost(nil)
		info := host.Info()

		if info.Namespace != EnvironmentNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, EnvironmentNamespace)
		}
	})

	t.Run("register returns functions", func(t *testing.T) {
		host := NewEnvironmentHost(nil)
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{
			"get-environment",
			"get-arguments",
			"initial-cwd",
		}

		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}
	})

	t.Run("no yield types for sync functions", func(t *testing.T) {
		host := NewEnvironmentHost(nil)
		reg := host.Register()

		if len(reg.YieldTypes) != 0 {
			t.Errorf("yield types = %d, want 0", len(reg.YieldTypes))
		}
	})
}

// mockEnvRegistry implements env.Registry for testing
type mockEnvRegistry struct {
	vars map[string]string
}

func (m *mockEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if v, ok := m.vars[name]; ok {
		return v, nil
	}
	return "", env.ErrVariableNotFound
}

func (m *mockEnvRegistry) Set(_ context.Context, name, value string) error {
	m.vars[name] = value
	return nil
}

func (m *mockEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	if v, ok := m.vars[name]; ok {
		return v, true, nil
	}
	return "", false, nil
}

func (m *mockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	result := make(map[string]string, len(m.vars))
	for k, v := range m.vars {
		result[k] = v
	}
	return result, nil
}

// allowAllScope implements security.Scope for testing
type allowAllScope struct{}

func (s *allowAllScope) With(_ security.Policy) security.Scope { return s }
func (s *allowAllScope) Without(_ registry.ID) security.Scope  { return s }
func (s *allowAllScope) Contains(_ registry.ID) bool           { return false }
func (s *allowAllScope) Policies() []security.Policy           { return nil }
func (s *allowAllScope) Evaluate(_ security.Actor, _, _ string, _ registry.Metadata) security.Result {
	return security.Allow
}

// denyEnvScope denies access to specific env vars
type denyEnvScope struct {
	denied map[string]bool
}

func (s *denyEnvScope) With(_ security.Policy) security.Scope { return s }
func (s *denyEnvScope) Without(_ registry.ID) security.Scope  { return s }
func (s *denyEnvScope) Contains(_ registry.ID) bool           { return false }
func (s *denyEnvScope) Policies() []security.Policy           { return nil }
func (s *denyEnvScope) Evaluate(_ security.Actor, _, resource string, _ registry.Metadata) security.Result {
	if s.denied[resource] {
		return security.Deny
	}
	return security.Allow
}

// setupSecurityContext creates a context with security actor and scope for testing
func setupSecurityContext(scope security.Scope) context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = security.SetActor(ctx, security.Actor{ID: "test"})
	_ = security.SetScope(ctx, scope)
	return ctx
}

func TestCollectEnvironment(t *testing.T) {
	t.Run("uses registry when available", func(t *testing.T) {
		host := NewEnvironmentHost(&Environment{
			Env: map[string]string{"STATIC": "value"},
		})

		mockReg := &mockEnvRegistry{
			vars: map[string]string{
				"FROM_REGISTRY": "registry_value",
				"ANOTHER":       "another_value",
			},
		}

		// Build context with registry and security
		ctx := setupSecurityContext(&allowAllScope{})
		ctx = env.WithRegistry(ctx, mockReg)

		result := host.collectEnvironment(ctx)

		if result["FROM_REGISTRY"] != "registry_value" {
			t.Errorf("FROM_REGISTRY = %q, want %q", result["FROM_REGISTRY"], "registry_value")
		}
		if result["ANOTHER"] != "another_value" {
			t.Errorf("ANOTHER = %q, want %q", result["ANOTHER"], "another_value")
		}
		// Static env should not be used when registry is available
		if _, ok := result["STATIC"]; ok {
			t.Error("static env should not be used when registry is available")
		}
	})

	t.Run("falls back to static env without registry", func(t *testing.T) {
		host := NewEnvironmentHost(&Environment{
			Env: map[string]string{
				"STATIC_VAR": "static_value",
			},
		})

		ctx := setupSecurityContext(&allowAllScope{})

		result := host.collectEnvironment(ctx)

		if result["STATIC_VAR"] != "static_value" {
			t.Errorf("STATIC_VAR = %q, want %q", result["STATIC_VAR"], "static_value")
		}
	})

	t.Run("filters by security permissions", func(t *testing.T) {
		host := NewEnvironmentHost(nil)

		mockReg := &mockEnvRegistry{
			vars: map[string]string{
				"ALLOWED":     "allowed_value",
				"SECRET_KEY":  "secret_value",
				"ANOTHER_VAR": "another_value",
			},
		}

		ctx := setupSecurityContext(&denyEnvScope{
			denied: map[string]bool{"SECRET_KEY": true},
		})
		ctx = env.WithRegistry(ctx, mockReg)

		result := host.collectEnvironment(ctx)

		if result["ALLOWED"] != "allowed_value" {
			t.Errorf("ALLOWED = %q, want %q", result["ALLOWED"], "allowed_value")
		}
		if _, ok := result["SECRET_KEY"]; ok {
			t.Error("SECRET_KEY should be filtered out by security")
		}
		if result["ANOTHER_VAR"] != "another_value" {
			t.Errorf("ANOTHER_VAR = %q, want %q", result["ANOTHER_VAR"], "another_value")
		}
	})

	t.Run("returns empty without security context", func(t *testing.T) {
		host := NewEnvironmentHost(&Environment{
			Env: map[string]string{"VAR": "value"},
		})

		ctx := context.Background()

		result := host.collectEnvironment(ctx)

		// Without security context, IsAllowed returns false, so all vars filtered
		if len(result) != 0 {
			t.Errorf("expected empty result without security, got %d vars", len(result))
		}
	})
}
