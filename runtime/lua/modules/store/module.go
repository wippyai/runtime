package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/store"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents a store Lua module
type Module struct {
	log *zap.Logger
}

// NewStoreModule creates and returns a new instance of the store Module
func NewStoreModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "store"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	// Create a simple module table with only the get function
	mod := l.CreateTable(0, 1)

	// Register get function
	mod.RawSetString("get", l.NewFunction(func(l *lua.LState) int {
		return storeGet(l, m.log)
	}))

	// Register Store type methods
	registerStore(l)

	// Push the module table
	l.Push(mod)
	return 1
}

// Store represents a store connection wrapper for Lua
type Store struct {
	resource  resource.Resource[any]
	store     store.Store
	log       *zap.Logger
	onRelease context.CancelFunc
}

// NewStore creates a new store connection wrapper with UoW integration
func NewStore(uw engine.UnitOfWork, resource resource.Resource[any], store store.Store, log *zap.Logger) *Store {
	storeWrapper := &Store{
		resource: resource,
		store:    store,
		log:      log,
	}

	// Register unconditional cleanup in UoW
	storeWrapper.onRelease = uw.AddCleanup(func() error {
		resource.Release()
		return nil
	})

	return storeWrapper
}

// registerStore registers the Store type and its methods
func registerStore(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"get":     storeGetValue,
		"set":     storeSetValue,
		"delete":  storeDelete,
		"has":     storeHas,
		"release": storeRelease,
	}

	value.RegisterMethods(l, "store.Store", methods)
}

// CheckStore checks if the first argument is a Store and returns it
func CheckStore(l *lua.LState) *Store {
	ud := l.CheckUserData(1)
	if storeObj, ok := ud.Value.(*Store); ok {
		return storeObj
	}
	l.ArgError(1, "expected store object")
	return nil
}

// WrapStore wraps a Store as a Lua userdata
func WrapStore(l *lua.LState, store *Store) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = store
	ud.Metatable = value.GetTypeMetatable(l, "store.Store")
	return ud
}

// storeGet retrieves a store resource by ID
func storeGet(l *lua.LState, log *zap.Logger) int {
	// Get resource ID
	id := l.CheckString(1)
	if id == "" {
		l.RaiseError("resource ID is required")
		return 0
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	reg := resource.GetResources(uw.Context())
	if reg == nil {
		l.RaiseError("resource registry not found")
		return 0
	}

	// Parse resource ID
	resID := registry.ParseID(id)

	// Acquire resource
	res, err := reg.Acquire(uw.Context(), resID, resource.ModeNormal)
	if err != nil {
		l.RaiseError("failed to acquire resource: %v", err)
		return 0
	}

	// Get Store instance
	storeRes, err := res.Get()
	if err != nil {
		res.Release()
		l.RaiseError("failed to get resource: %v", err)
		return 0
	}

	// Check if it's a Store implementation
	storeImpl, ok := storeRes.(store.Store)
	if !ok {
		res.Release()
		l.RaiseError("resource is not a store: %T", storeRes)
		return 0
	}

	// Create and wrap Store with UoW integration
	storeObj := NewStore(uw, res, storeImpl, log)

	// Create userdata
	ud := WrapStore(l, storeObj)
	l.Push(ud)
	return 1
}

// storeGetValue retrieves a value from the store
func storeGetValue(l *lua.LState) int {
	// Check and get store
	storeObj := CheckStore(l)
	if storeObj == nil {
		return 0
	}

	// Get key
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key is required")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Parse key
		parsedKey := registry.ParseID(key)

		// Get value from store
		val, err := storeObj.store.Get(l.Context(), parsedKey)
		if err != nil {
			if err == store.ErrKeyNotFound {
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("key not found")}, nil)
			}
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Use payload transcoder to convert to Lua
		transcoder := payload.GetTranscoder(l.Context())
		if transcoder == nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("transcoder not found in context")}, nil)
		}

		// Try to transcode to Lua format first
		luaPayload, err := transcoder.Transcode(val, payload.Lua)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("failed to transcode: %v", err))}, nil)
		}

		// Extract the Lua value from the payload
		luaVal, ok := luaPayload.Data().(lua.LValue)
		if !ok {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("invalid payload data type: %T", luaPayload.Data()))}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{luaVal, lua.LNil}, nil)
	})

	return -1 // Yield
}

// storeSetValue sets a value in the store
func storeSetValue(l *lua.LState) int {
	// Check and get store
	storeObj := CheckStore(l)
	if storeObj == nil {
		return 0
	}

	// Get key
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key is required")
		return 0
	}

	// Get value
	luaVal := l.Get(3)
	if luaVal == lua.LNil {
		l.RaiseError("value is required")
		return 0
	}

	// Get TTL (optional)
	ttl := time.Duration(0)
	if l.GetTop() >= 4 {
		ttlSeconds := l.CheckNumber(4)
		ttl = time.Duration(float64(ttlSeconds) * float64(time.Second))
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Parse key
		parsedKey := registry.ParseID(key)

		// Use payload transcoder to convert from Lua
		transcoder := payload.GetTranscoder(l.Context())
		if transcoder == nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("transcoder not found in context")}, nil)
		}

		// Create a Lua payload
		luaPayload := payload.NewPayload(luaVal, payload.Lua)

		// Transcode to Golang format
		goPayload, err := transcoder.Transcode(luaPayload, payload.Golang)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("failed to transcode: %v", err))}, nil)
		}

		// Create entry
		entry := store.Entry{
			Key:   parsedKey,
			Value: goPayload,
			TTL:   ttl,
		}

		// Set value in store
		err = storeObj.store.Set(l.Context(), entry)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}

// storeDelete deletes a value from the store
func storeDelete(l *lua.LState) int {
	// Check and get store
	storeObj := CheckStore(l)
	if storeObj == nil {
		return 0
	}

	// Get key
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key is required")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Parse key
		parsedKey := registry.ParseID(key)

		// Delete value from store
		err := storeObj.store.Delete(l.Context(), parsedKey)
		if err != nil {
			if err == store.ErrKeyNotFound {
				return engine.NewUpdate(nil, []lua.LValue{lua.LFalse, lua.LNil}, nil)
			}
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}

// storeHas checks if a key exists in the store
func storeHas(l *lua.LState) int {
	// Check and get store
	storeObj := CheckStore(l)
	if storeObj == nil {
		return 0
	}

	// Get key
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key is required")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Parse key
		parsedKey := registry.ParseID(key)

		// Check if key exists
		exists, err := storeObj.store.Has(l.Context(), parsedKey)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LBool(exists), lua.LNil}, nil)
	})

	return -1 // Yield
}

// storeRelease releases a store resource
func storeRelease(l *lua.LState) int {
	// Check and get store
	storeObj := CheckStore(l)
	if storeObj == nil {
		return 0
	}

	// Release the resource directly
	if storeObj.resource != nil {
		storeObj.resource.Release()
		storeObj.resource = nil
	}

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if storeObj.onRelease != nil {
		storeObj.onRelease()
		storeObj.onRelease = nil
	}

	l.Push(lua.LTrue)
	return 1
}
