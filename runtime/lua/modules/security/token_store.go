package security

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	securityapi "github.com/wippyai/runtime/api/dispatcher/security"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	luasec "github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const tokenStoreTypeName = "security.TokenStore"

// TokenStore wraps a token store resource with cleanup handling.
type TokenStore struct {
	id            registry.ID
	resource      resource.Resource[any]
	tokenStore    secapi.TokenStore
	released      bool
	mu            sync.Mutex
	cancelCleanup func()
}

// NewTokenStore creates a new token store wrapper.
func NewTokenStore(ctx context.Context, id registry.ID, res resource.Resource[any], ts secapi.TokenStore) *TokenStore {
	wrapper := &TokenStore{
		id:         id,
		resource:   res,
		tokenStore: ts,
		released:   false,
	}

	store := rtresource.GetStore(ctx)
	if store != nil {
		wrapper.cancelCleanup = store.AddCleanup(func() error {
			wrapper.mu.Lock()
			defer wrapper.mu.Unlock()
			if !wrapper.released && wrapper.resource != nil {
				wrapper.resource.Release()
				wrapper.released = true
			}
			return nil
		})
	}

	return wrapper
}

var tokenStoreMethods = map[string]lua.LGFunction{
	"validate": tokenStoreValidate,
	"create":   tokenStoreCreate,
	"revoke":   tokenStoreRevoke,
	"close":    tokenStoreClose,
}

func checkTokenStore(l *lua.LState, idx int) *TokenStore {
	ud := l.CheckUserData(idx)
	if ts, ok := ud.Value.(*TokenStore); ok {
		return ts
	}
	l.ArgError(idx, "TokenStore expected")
	return nil
}

// tokenStoreGet acquires a token store resource.
func tokenStoreGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	idStr := l.CheckString(1)
	if idStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("token store id is required"))
		return 2
	}

	if !luasec.IsAllowed(ctx, "security.token_store.get", idStr, nil) {
		l.RaiseError("not allowed to access token store: %s", idStr)
		return 0
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	id := registry.ParseID(idStr)
	res, err := reg.Acquire(ctx, id, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	storeRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	tokenStore, ok := storeRes.(secapi.TokenStore)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString("resource is not a token store"))
		return 2
	}

	ts := NewTokenStore(ctx, id, res, tokenStore)
	ud := l.NewUserData()
	ud.Value = ts
	ud.Metatable = value.GetTypeMetatable(l, tokenStoreTypeName)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// tokenStoreValidate yields to validate a token.
func tokenStoreValidate(l *lua.LState) int {
	ts := checkTokenStore(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	if ts.released {
		ts.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.LString("token store is closed"))
		return 3
	}
	tokenStore := ts.tokenStore
	storeID := ts.id.String()
	ts.mu.Unlock()

	tokenStr := l.CheckString(2)
	token := secapi.Token(tokenStr)

	meta := registry.Metadata{"token": tokenStr}
	if !luasec.IsAllowed(l.Context(), "security.token.validate", storeID, meta) {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to validate token"))
		return 3
	}

	yield := acquireValidateYield(tokenStore, token)
	l.Push(yield)
	return -1
}

// tokenStoreCreate yields to create a new token.
func tokenStoreCreate(l *lua.LState) int {
	ts := checkTokenStore(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	if ts.released {
		ts.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("token store is closed"))
		return 2
	}
	tokenStore := ts.tokenStore
	storeID := ts.id.String()
	ts.mu.Unlock()

	// Get actor
	actorUD := l.CheckUserData(2)
	actor, ok := actorUD.Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	// Get scope
	scopeUD := l.CheckUserData(3)
	scope, ok := scopeUD.Value.(secapi.Scope)
	if !ok {
		l.ArgError(3, "Scope expected")
		return 0
	}

	meta := registry.Metadata{"actor": actor.ID}
	if !luasec.IsAllowed(l.Context(), "security.token.create", storeID, meta) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to create token"))
		return 2
	}

	// Parse options
	var expiration time.Duration
	var tokenMeta registry.Metadata
	if l.GetTop() >= 4 {
		optionsTable := l.CheckTable(4)

		// Parse expiration
		if exp := optionsTable.RawGetString("expiration"); exp != lua.LNil {
			switch v := exp.(type) {
			case lua.LString:
				d, err := time.ParseDuration(string(v))
				if err != nil {
					l.Push(lua.LNil)
					l.Push(lua.LString("invalid expiration format"))
					return 2
				}
				expiration = d
			case lua.LNumber:
				expiration = time.Duration(v) * time.Millisecond
			}
		}

		// Parse metadata
		if metaValue := optionsTable.RawGetString("meta"); metaValue != lua.LNil {
			if metaTable, ok := metaValue.(*lua.LTable); ok {
				tokenMeta = luaTableToMetadata(l, metaTable)
			}
		}
	}

	details := secapi.TokenDetails{
		Expiration: expiration,
		Meta:       tokenMeta,
	}

	yield := acquireCreateYield(tokenStore, actor, scope, details)
	l.Push(yield)
	return -1
}

// tokenStoreRevoke yields to revoke a token.
func tokenStoreRevoke(l *lua.LState) int {
	ts := checkTokenStore(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	if ts.released {
		ts.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("token store is closed"))
		return 2
	}
	tokenStore := ts.tokenStore
	storeID := ts.id.String()
	ts.mu.Unlock()

	tokenStr := l.CheckString(2)
	token := secapi.Token(tokenStr)

	meta := registry.Metadata{"token": tokenStr}
	if !luasec.IsAllowed(l.Context(), "security.token.revoke", storeID, meta) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to revoke token"))
		return 2
	}

	yield := acquireRevokeYield(tokenStore, token)
	l.Push(yield)
	return -1
}

// tokenStoreClose releases the token store resource.
func tokenStoreClose(l *lua.LState) int {
	ts := checkTokenStore(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	if !ts.released && ts.resource != nil {
		ts.resource.Release()
		ts.resource = nil
		ts.released = true
		cancel := ts.cancelCleanup
		ts.cancelCleanup = nil
		ts.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	} else {
		ts.mu.Unlock()
	}

	l.Push(lua.LTrue)
	return 1
}

func tokenStoreToString(l *lua.LState) int {
	ts := checkTokenStore(l, 1)
	if ts == nil {
		return 0
	}
	ts.mu.Lock()
	released := ts.released
	ts.mu.Unlock()

	if released {
		l.Push(lua.LString("security.TokenStore{closed}"))
	} else {
		l.Push(lua.LString("security.TokenStore{}"))
	}
	return 1
}

// Yield types

// ValidateYield is yielded to validate a token.
type ValidateYield struct {
	TokenStore secapi.TokenStore
	Token      secapi.Token
}

var validateYieldPool = sync.Pool{New: func() any { return &ValidateYield{} }}

func acquireValidateYield(ts secapi.TokenStore, token secapi.Token) *ValidateYield {
	y := validateYieldPool.Get().(*ValidateYield)
	y.TokenStore = ts
	y.Token = token
	return y
}

func releaseValidateYield(y *ValidateYield) {
	y.TokenStore = nil
	y.Token = ""
	validateYieldPool.Put(y)
}

func (y *ValidateYield) Release()                    { releaseValidateYield(y) }
func (y *ValidateYield) String() string              { return "<token_validate_yield>" }
func (y *ValidateYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *ValidateYield) CmdID() dispatcher.CommandID { return securityapi.CmdTokenValidate }
func (y *ValidateYield) ToCommand() dispatcher.Command {
	cmd := securityapi.AcquireTokenValidateCmd()
	cmd.TokenStore = y.TokenStore
	cmd.Token = y.Token
	return cmd
}

func (y *ValidateYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(securityapi.TokenValidateResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{wrapActor(l, resp.Actor), wrapScope(l, resp.Scope), lua.LNil}
}

// CreateYield is yielded to create a token.
type CreateYield struct {
	TokenStore secapi.TokenStore
	Actor      secapi.Actor
	Scope      secapi.Scope
	Details    secapi.TokenDetails
}

var createYieldPool = sync.Pool{New: func() any { return &CreateYield{} }}

func acquireCreateYield(ts secapi.TokenStore, actor secapi.Actor, scope secapi.Scope, details secapi.TokenDetails) *CreateYield {
	y := createYieldPool.Get().(*CreateYield)
	y.TokenStore = ts
	y.Actor = actor
	y.Scope = scope
	y.Details = details
	return y
}

func releaseCreateYield(y *CreateYield) {
	y.TokenStore = nil
	y.Actor = secapi.Actor{}
	y.Scope = nil
	y.Details = secapi.TokenDetails{}
	createYieldPool.Put(y)
}

func (y *CreateYield) Release()                    { releaseCreateYield(y) }
func (y *CreateYield) String() string              { return "<token_create_yield>" }
func (y *CreateYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *CreateYield) CmdID() dispatcher.CommandID { return securityapi.CmdTokenCreate }
func (y *CreateYield) ToCommand() dispatcher.Command {
	cmd := securityapi.AcquireTokenCreateCmd()
	cmd.TokenStore = y.TokenStore
	cmd.Actor = y.Actor
	cmd.Scope = y.Scope
	cmd.Details = y.Details
	return cmd
}

func (y *CreateYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(securityapi.TokenCreateResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LString(resp.Token), lua.LNil}
}

// RevokeYield is yielded to revoke a token.
type RevokeYield struct {
	TokenStore secapi.TokenStore
	Token      secapi.Token
}

var revokeYieldPool = sync.Pool{New: func() any { return &RevokeYield{} }}

func acquireRevokeYield(ts secapi.TokenStore, token secapi.Token) *RevokeYield {
	y := revokeYieldPool.Get().(*RevokeYield)
	y.TokenStore = ts
	y.Token = token
	return y
}

func releaseRevokeYield(y *RevokeYield) {
	y.TokenStore = nil
	y.Token = ""
	revokeYieldPool.Put(y)
}

func (y *RevokeYield) Release()                    { releaseRevokeYield(y) }
func (y *RevokeYield) String() string              { return "<token_revoke_yield>" }
func (y *RevokeYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *RevokeYield) CmdID() dispatcher.CommandID { return securityapi.CmdTokenRevoke }
func (y *RevokeYield) ToCommand() dispatcher.Command {
	cmd := securityapi.AcquireTokenRevokeCmd()
	cmd.TokenStore = y.TokenStore
	cmd.Token = y.Token
	return cmd
}

func (y *RevokeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(securityapi.TokenRevokeResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// getTokenStoreFromResource extracts the token store from a resource.
func getTokenStoreFromResource(res resource.Resource[any]) (secapi.TokenStore, error) {
	storeImpl, err := res.Get()
	if err != nil {
		return nil, err
	}

	tokenStore, ok := storeImpl.(secapi.TokenStore)
	if !ok {
		return nil, errors.New("resource is not a token store")
	}

	return tokenStore, nil
}
