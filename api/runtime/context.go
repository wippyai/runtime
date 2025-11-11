package runtime

import (
	ctxapi "github.com/ponyruntime/pony/api/context"
)

// CallIDKey is the key for storing the registry ID of the called function/process.
// The value type is registry.ID (contains NS and Name).
//
// Usage:
//
//	cc := ctxapi.CallFromContext(ctx)
//	err := cc.Set(CallIDKey, task.ID)  // returns error if already set
//	id, exists := cc.Get(CallIDKey)
var CallIDKey = &ctxapi.Key{Name: "runtime.call_id", Scope: ctxapi.ScopeCall}

// CallPIDKey is the key for storing the unique call instance identifier.
// The value type is string (e.g., "0x00001").
//
// Usage:
//
//	cc := ctxapi.CallFromContext(ctx)
//	err := cc.Set(CallPIDKey, uniqID)  // returns error if already set
//	pidValue, exists := cc.Get(CallPIDKey)
var CallPIDKey = &ctxapi.Key{Name: "runtime.call_pid", Scope: ctxapi.ScopeCall}
