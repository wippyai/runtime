package security

import (
	"context"
	"errors"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const TokenStoreMetatable = "security.TokenStore"

// TokenStore wraps a security.TokenStore with resource handling
type TokenStore struct {
	resource   resource.Resource[any]
	tokenStore secapi.TokenStore
	log        *zap.Logger
	onRelease  context.CancelFunc // Cancel function from UoW
}

// NewTokenStore creates a new token store wrapper with UoW integration
func NewTokenStore(uw engine.UnitOfWork, res resource.Resource[any], tokenStore secapi.TokenStore, log *zap.Logger) *TokenStore {
	wrapper := &TokenStore{
		resource:   res,
		tokenStore: tokenStore,
		log:        log,
	}

	// Register cleanup in UoW
	wrapper.onRelease = uw.AddCleanup(func() error {
		if wrapper.resource != nil {
			wrapper.resource.Release()
			wrapper.resource = nil
		}
		return nil
	})

	return wrapper
}

// wrapTokenStore wraps a TokenStore as a Lua userdata
func wrapTokenStore(l *lua.LState, tokenStore *TokenStore) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = tokenStore
	ud.Metatable = value.GetTypeMetatable(l, TokenStoreMetatable)
	return ud
}

// checkTokenStore checks if the first argument is a TokenStore and returns it
func checkTokenStore(l *lua.LState) *TokenStore {
	ud := l.CheckUserData(1)
	if ts, ok := ud.Value.(*TokenStore); ok {
		return ts
	}
	l.ArgError(1, "TokenStore expected")
	return nil
}

// registerTokenStoreType registers the TokenStore type and methods
func registerTokenStoreType(l *lua.LState) {
	value.RegisterMethods(l, TokenStoreMetatable, map[string]lua.LGFunction{
		"validate": tokenStoreValidate,
		"create":   tokenStoreCreate,
		"revoke":   tokenStoreRevoke,
		"close":    tokenStoreClose,
	})
}

// tokenStoreValidate validates a token
func tokenStoreValidate(l *lua.LState) int {
	ts := checkTokenStore(l)
	if ts == nil {
		return 0
	}

	if ts.resource == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.LString("token store is closed"))
		return 3
	}

	tokenStr := l.CheckString(2)
	token := secapi.Token(tokenStr)

	// Validate the token
	actor, scope, err := ts.tokenStore.Validate(l.Context(), token)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 3
	}

	// Return actor and scope
	actorUD := wrapActor(l, actor)
	scopeUD := wrapScope(l, scope)

	l.Push(actorUD)
	l.Push(scopeUD)
	l.Push(lua.LNil)
	return 3
}

// tokenStoreCreate creates a new token
func tokenStoreCreate(l *lua.LState) int {
	ts := checkTokenStore(l)
	if ts == nil {
		return 0
	}

	if ts.resource == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("token store is closed"))
		return 2
	}

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

	// Get options
	optionsTable := l.OptTable(4, l.NewTable())

	// Parse expiration
	var expiration time.Duration
	if exp := optionsTable.RawGetString("expiration"); exp != lua.LNil {
		var err error
		switch exp.Type() {
		case lua.LTString:
			expiration, err = time.ParseDuration(exp.String())
			if err != nil {
				l.Push(lua.LNil)
				l.Push(lua.LString("invalid expiration: " + err.Error()))
				return 2
			}
		case lua.LTNumber:
			// Assume milliseconds
			expiration = time.Duration(exp.(lua.LNumber)) * time.Millisecond
		default:
			l.Push(lua.LNil)
			l.Push(lua.LString("expiration must be string or number"))
			return 2
		}
	}

	// Parse metadata from the options table's "meta" field
	meta := registry.Metadata{}
	if metaValue := optionsTable.RawGetString("meta"); metaValue != lua.LNil {
		if metaTable, ok := metaValue.(*lua.LTable); ok {
			var err error
			meta, err = luaTableToMetadata(l, metaTable)
			if err != nil {
				l.RaiseError(err.Error())
				return 0
			}
		}
	}

	// Create token details
	details := secapi.TokenDetails{
		Expiration: expiration,
		Meta:       meta,
	}

	// This function is asynchronous, so wrap it with coroutine
	coroutine.Wrap(l, func() *engine.Update {
		// Create the token
		token, err := ts.tokenStore.Create(l.Context(), actor, scope, details)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LString(token), lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}

// tokenStoreCreate creates a new token
func tokenStoreRevoke(l *lua.LState) int {
	ts := checkTokenStore(l)
	if ts == nil {
		return 0
	}

	if ts.resource == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("token store is closed"))
		return 2
	}

	token := l.CheckString(2)

	err := ts.tokenStore.Revoke(l.Context(), secapi.Token(token))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to revoke token: " + err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// tokenStoreClose closes the token store resource
func tokenStoreClose(l *lua.LState) int {
	ts := checkTokenStore(l)
	if ts == nil {
		return 0
	}

	// Release the resource if it's not already released
	if ts.resource != nil {
		ts.resource.Release()
		ts.resource = nil
	}

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if ts.onRelease != nil {
		ts.onRelease()
		ts.onRelease = nil
	}

	l.Push(lua.LTrue)
	return 1
}

// getTokenStoreFromResource extracts the token store from a resource
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
