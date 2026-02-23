// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
)

// mockPolicy implements secapi.Policy for testing
type mockPolicy struct {
	id     registry.ID
	effect secapi.Result
}

func newMockPolicy(ns, name string, effect secapi.Result) *mockPolicy {
	return &mockPolicy{
		id:     registry.NewID(ns, name),
		effect: effect,
	}
}

func (p *mockPolicy) ID() registry.ID {
	return p.id
}

func (p *mockPolicy) Evaluate(_ secapi.Actor, _, _ string, _ attrs.Bag) secapi.Result {
	return p.effect
}

func setupState() *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func setupStateWithSecurityContext(actor secapi.Actor, scope secapi.Scope) *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	ctx, _ = ctxapi.OpenFrameContext(ctx)

	_ = secapi.SetActor(ctx, actor)
	_ = secapi.SetScope(ctx, scope)

	l.SetContext(ctx)
	return l
}

func TestModuleLoads(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("security")
	if mod.Type() != lua.LTTable {
		t.Fatal("security module not registered")
	}

	modTbl := mod.(*lua.LTable)
	funcs := []string{"actor", "scope", "can", "policy", "named_scope", "new_scope", "new_actor", "token_store"}
	for _, fn := range funcs {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestModuleReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("security").(*lua.LTable)
	mod2 := l2.GetGlobal("security").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestModuleImmutable(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("security").(*lua.LTable)
	if !mod.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestActorReturnsNilWithoutContext(t *testing.T) {
	l := setupState()
	defer l.Close()
	l.SetContext(context.Background())

	err := l.DoString(`
		local a = security.actor()
		if a ~= nil then
			error("expected nil when no actor in context")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestActorReturnsActor(t *testing.T) {
	actor := secapi.Actor{ID: "test-user", Meta: map[string]any{"role": "admin"}}
	scope := secsystem.NewScope(nil)
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.actor()
		if a == nil then
			error("expected actor")
		end
		if a:id() ~= "test-user" then
			error("expected actor id 'test-user', got: " .. a:id())
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestActorMeta(t *testing.T) {
	actor := secapi.Actor{ID: "user1", Meta: map[string]any{"role": "admin", "level": 5}}
	scope := secsystem.NewScope(nil)
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.actor()
		local meta = a:meta()
		if type(meta) ~= "table" then
			error("expected table, got: " .. type(meta))
		end
		if meta.role ~= "admin" then
			error("expected role 'admin'")
		end
		if meta.level ~= 5 then
			error("expected level 5")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestScopeReturnsNilWithoutContext(t *testing.T) {
	l := setupState()
	defer l.Close()
	l.SetContext(context.Background())

	err := l.DoString(`
		local s = security.scope()
		if s ~= nil then
			error("expected nil when no scope in context")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestScopeReturnsScope(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	scope := secsystem.NewScope(nil)
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local s = security.scope()
		if s == nil then
			error("expected scope")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestScopePolicies(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "allow-read", secapi.Allow)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local s = security.scope()
		local policies = s:policies()
		if type(policies) ~= "table" then
			error("expected table, got: " .. type(policies))
		end
		if #policies ~= 1 then
			error("expected 1 policy, got: " .. #policies)
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestScopeContains(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "allow-read", secapi.Allow)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local s = security.scope()
		if not s:contains("test:allow-read") then
			error("scope should contain test:allow-read")
		end
		if s:contains("test:nonexistent") then
			error("scope should not contain test:nonexistent")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestCanReturnsFalseWithoutContext(t *testing.T) {
	l := setupState()
	defer l.Close()
	l.SetContext(context.Background())

	err := l.DoString(`
		local allowed = security.can("read", "resource")
		if allowed then
			error("expected false when no security context")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestCanWithAllowPolicy(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "allow-all", secapi.Allow)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local allowed = security.can("read", "resource")
		if not allowed then
			error("expected true with allow policy")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestCanWithDenyPolicy(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "deny-all", secapi.Deny)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local allowed = security.can("read", "resource")
		if allowed then
			error("expected false with deny policy")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPolicyID(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("myns", "mypol", secapi.Allow)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local s = security.scope()
		local policies = s:policies()
		local p = policies[1]
		local id = p:id()
		if id ~= "myns:mypol" then
			error("expected 'myns:mypol', got: " .. id)
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPolicyEvaluate(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "allow-policy", secapi.Allow)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.actor()
		local s = security.scope()
		local policies = s:policies()
		local p = policies[1]
		local result = p:evaluate(a, "read", "resource")
		if result ~= "allow" then
			error("expected 'allow', got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestScopeEvaluate(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "allow-policy", secapi.Allow)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.actor()
		local s = security.scope()
		local result = s:evaluate(a, "read", "resource")
		if result ~= "allow" then
			error("expected 'allow', got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
