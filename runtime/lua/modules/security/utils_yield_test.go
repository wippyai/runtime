package security

import (
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- tableToMap ---

func TestTableToMap_Empty(t *testing.T) {
	tbl := lua.CreateTable(0, 0)
	result := tableToMap(tbl)
	assert.Empty(t, result)
}

func TestTableToMap_StringKeysOnly(t *testing.T) {
	tbl := lua.CreateTable(1, 1)
	tbl.RawSetString("str", lua.LString("value"))
	tbl.RawSetInt(1, lua.LString("indexed"))

	result := tableToMap(tbl)
	assert.Equal(t, "value", result["str"])
	assert.Nil(t, result["1"])
}

func TestTableToMap_NestedTables(t *testing.T) {
	inner := lua.CreateTable(0, 1)
	inner.RawSetString("deep", lua.LTrue)

	outer := lua.CreateTable(0, 1)
	outer.RawSetString("inner", inner)

	result := tableToMap(outer)
	nested, ok := result["inner"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, nested["deep"])
}

// --- ValidateYield ---

func TestValidateYield_Type(t *testing.T) {
	y := &ValidateYield{}
	assert.Equal(t, lua.LTUserData, y.Type())
	assert.Equal(t, "<token_validate_yield>", y.String())
	assert.Equal(t, secapi.ValidateToken, y.CmdID())
}

func TestValidateYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ValidateYield{}
	result := y.HandleResult(l, nil, errors.New("validation failed"))

	require.Len(t, result, 3)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
	assert.NotEqual(t, lua.LNil, result[2])
}

func TestValidateYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ValidateYield{}
	result := y.HandleResult(l, "not a response", nil)

	require.Len(t, result, 3)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
	assert.NotEqual(t, lua.LNil, result[2])
}

func TestValidateYield_HandleResult_ResponseError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ValidateYield{}
	resp := secapi.ValidateTokenResponse{Error: errors.New("token expired")}
	result := y.HandleResult(l, resp, nil)

	require.Len(t, result, 3)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
	assert.NotEqual(t, lua.LNil, result[2])
}

func TestValidateYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	actor := secapi.Actor{ID: "user-1", Meta: map[string]any{"role": "admin"}}
	scope := &fakeScope{}
	resp := secapi.ValidateTokenResponse{Actor: actor, Scope: scope}

	y := &ValidateYield{}
	result := y.HandleResult(l, resp, nil)

	require.Len(t, result, 3)
	assert.NotEqual(t, lua.LNil, result[0]) // actor
	assert.NotEqual(t, lua.LNil, result[1]) // scope
	assert.Equal(t, lua.LNil, result[2])    // no error
}

func TestValidateYield_AcquireRelease(t *testing.T) {
	y := acquireValidateYield(nil, "tok-123")
	assert.Equal(t, secapi.Token("tok-123"), y.Token)
	releaseValidateYield(y)
	assert.Empty(t, y.Token)
	assert.Nil(t, y.TokenStore)
}

// --- CreateYield ---

func TestCreateYield_Type(t *testing.T) {
	y := &CreateYield{}
	assert.Equal(t, lua.LTUserData, y.Type())
	assert.Equal(t, "<token_create_yield>", y.String())
	assert.Equal(t, secapi.CreateToken, y.CmdID())
}

func TestCreateYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &CreateYield{}
	result := y.HandleResult(l, nil, errors.New("create failed"))

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestCreateYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &CreateYield{}
	result := y.HandleResult(l, "not a response", nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestCreateYield_HandleResult_ResponseError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &CreateYield{}
	resp := secapi.CreateTokenResponse{Error: errors.New("duplicate")}
	result := y.HandleResult(l, resp, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestCreateYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &CreateYield{}
	resp := secapi.CreateTokenResponse{Token: "new-token-abc"}
	result := y.HandleResult(l, resp, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LString("new-token-abc"), result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestCreateYield_AcquireRelease(t *testing.T) {
	actor := secapi.Actor{ID: "test"}
	scope := &fakeScope{}
	details := secapi.TokenDetails{}
	y := acquireCreateYield(nil, actor, scope, details)
	assert.Equal(t, actor, y.Actor)
	assert.Equal(t, scope, y.Scope)
	releaseCreateYield(y)
	assert.Nil(t, y.TokenStore)
	assert.Nil(t, y.Scope)
}

// --- RevokeYield ---

func TestRevokeYield_Type(t *testing.T) {
	y := &RevokeYield{}
	assert.Equal(t, lua.LTUserData, y.Type())
	assert.Equal(t, "<token_revoke_yield>", y.String())
	assert.Equal(t, secapi.RevokeToken, y.CmdID())
}

func TestRevokeYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &RevokeYield{}
	result := y.HandleResult(l, nil, errors.New("revoke failed"))

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestRevokeYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &RevokeYield{}
	result := y.HandleResult(l, "not a response", nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestRevokeYield_HandleResult_ResponseError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &RevokeYield{}
	resp := secapi.RevokeTokenResponse{Error: errors.New("not found")}
	result := y.HandleResult(l, resp, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestRevokeYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &RevokeYield{}
	resp := secapi.RevokeTokenResponse{}
	result := y.HandleResult(l, resp, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestRevokeYield_AcquireRelease(t *testing.T) {
	y := acquireRevokeYield(nil, "tok-456")
	assert.Equal(t, secapi.Token("tok-456"), y.Token)
	releaseRevokeYield(y)
	assert.Empty(t, y.Token)
	assert.Nil(t, y.TokenStore)
}

// --- Scope operations via Lua ---

func TestNewScope_CreatesScope(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "rule-1", secapi.Allow)
	scope := &fakeScope{policies: []secapi.Policy{pol}}
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local s = security.scope()
		local policies = s:policies()
		assert(#policies == 1, "expected 1 policy, got " .. #policies)
		local new_s = security.new_scope(policies)
		assert(new_s ~= nil, "expected new scope")
	`)
	assert.NoError(t, err)
}

func TestNewActor_CreatesActor(t *testing.T) {
	actor := secapi.Actor{ID: "admin"}
	pol := newMockPolicy("test", "allow-all", secapi.Allow)
	scope := &fakeScope{policies: []secapi.Policy{pol}}
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.new_actor("new-user", {role = "viewer"})
		assert(a ~= nil, "expected actor")
		assert(a:id() == "new-user", "wrong id: " .. a:id())
		local meta = a:meta()
		assert(meta.role == "viewer", "wrong role")
	`)
	assert.NoError(t, err)
}

func TestNewActor_PreservesComplexMetadata(t *testing.T) {
	actor := secapi.Actor{ID: "admin"}
	pol := newMockPolicy("test", "allow-all", secapi.Allow)
	scope := &fakeScope{policies: []secapi.Policy{pol}}
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.new_actor("new-user", {
			organization_id = "org-123",
			org_role_id = 7,
			org_role_name = "owner",
			org_permissions = {"read", "write"},
			org = {active = true, level = 3}
		})
		assert(a ~= nil, "expected actor")
		local meta = a:meta()
		assert(meta.organization_id == "org-123", "missing organization_id")
		assert(meta.org_role_id == 7, "missing org_role_id")
		assert(meta.org_role_name == "owner", "missing org_role_name")
		assert(type(meta.org_permissions) == "table", "org_permissions should be table")
		assert(#meta.org_permissions == 2, "org_permissions length mismatch")
		assert(meta.org_permissions[1] == "read", "org_permissions[1] mismatch")
		assert(meta.org_permissions[2] == "write", "org_permissions[2] mismatch")
		assert(type(meta.org) == "table", "org should be table")
		assert(meta.org.active == true, "org.active mismatch")
		assert(meta.org.level == 3, "org.level mismatch")
	`)
	assert.NoError(t, err)
}

func TestScopeWith_AddsPolicy(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "rule-1", secapi.Allow)
	scope := &fakeScope{policies: []secapi.Policy{pol}}
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local s = security.scope()
		local policies = s:policies()
		local p = policies[1]
		local new_s = s:with(p)
		assert(new_s ~= nil, "expected new scope from :with()")
	`)
	assert.NoError(t, err)
}

func TestScopeEvaluate_Deny(t *testing.T) {
	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "deny-all", secapi.Deny)
	scope := &fakeScope{policies: []secapi.Policy{pol}}
	l := setupStateWithSecurityContext(actor, scope)
	defer l.Close()

	err := l.DoString(`
		local a = security.actor()
		local s = security.scope()
		local result = s:evaluate(a, "write", "resource")
		assert(result == "deny", "expected deny, got: " .. tostring(result))
	`)
	assert.NoError(t, err)
}

// fakeScope implements secapi.Scope for testing
type fakeScope struct {
	policies []secapi.Policy
}

func (s *fakeScope) Evaluate(actor secapi.Actor, action, resource string, meta attrs.Bag) secapi.Result {
	for _, p := range s.policies {
		r := p.Evaluate(actor, action, resource, meta)
		if r != secapi.Undefined {
			return r
		}
	}
	return secapi.Undefined
}

func (s *fakeScope) With(policy secapi.Policy) secapi.Scope {
	return &fakeScope{policies: append(s.policies, policy)}
}

func (s *fakeScope) Without(id registry.ID) secapi.Scope {
	var remaining []secapi.Policy
	for _, p := range s.policies {
		if p.ID() != id {
			remaining = append(remaining, p)
		}
	}
	return &fakeScope{policies: remaining}
}

func (s *fakeScope) Contains(id registry.ID) bool {
	for _, p := range s.policies {
		if p.ID() == id {
			return true
		}
	}
	return false
}

func (s *fakeScope) Policies() []secapi.Policy {
	return s.policies
}
