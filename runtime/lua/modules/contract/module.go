package contract

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/command"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
	"github.com/ponyruntime/pony/runtime/lua/security"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Security permission constants for contract operations
const (
	PermissionContractGet             = "contract.get"
	PermissionContractImplementations = "contract.implementations.list"
	PermissionContractBindingOpen     = "contract.binding.open"
	PermissionContractMethodCall      = "contract.method.call"
	PermissionContractMethodAccess    = "contract.method.access"
	PermissionContractSecurityContext = "contract.security"
	PermissionContractAppContext      = "contract.context"
)

// Type names for Lua userdata registration
const (
	TypeNameContract = "contract.Contract"
	TypeNameInstance = "contract.Instance"
)

// callContext holds both security and application context for contract method calls
// This combines security context (actor, scope) with application context (custom values)
// following the same pattern as the funcs module
type callContext struct {
	values   *contextapi.Contexter[any] // Application context values
	actor    secapi.Actor               // Security actor
	hasActor bool                       // Whether actor is set
	scope    secapi.Scope               // Security scope
	hasScope bool                       // Whether scope is set
}

// Module represents the contract module for Lua runtime
type Module struct {
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
}

// Wrapper holds contract definition with call context (like Functions in funcs module)
// Supports immutable chaining of security context through with_* methods
type Wrapper struct {
	definition  contract.Contract     // Contract definition from registry
	registry    contract.Registry     // Contract registry for lookups
	inst        contract.Instantiator // Instantiator for creating instances
	callContext                       // Embedded call context for security + app context
}

// InstanceWrapper holds contract instance with inherited call context
// All method calls on this instance will use the inherited security and application context
type InstanceWrapper struct {
	instance        contract.Instance // Actual contract instance
	owningContracts map[string]string // method name -> contract ID mapping for fast lookup
	callContext                       // Inherited call context from Wrapper
}

// NewContractModule creates a new contract module for the Lua runtime
func NewContractModule(log *zap.Logger) *Module {
	return &Module{log: log.Named("contract")}
}

// Name returns the module name for registration
func (m *Module) Name() string {
	return "contract"
}

// Loader registers the contract module types and functions with the Lua state
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Register contract type with call context chain methods (like funcs module)
	value.RegisterTypeMethods(l, TypeNameContract, nil, map[string]lua.LGFunction{
		// Core contract introspection methods
		"id":              contractID,
		"methods":         contractMethods,
		"method":          contractMethod,
		"implementations": contractImplementations,
		"open":            contractOpen,

		// Call context chain methods
		"with_context": m.withContext,
		"with_actor":   m.withActor,
		"with_scope":   m.withScope,
	})

	// Register instance type with dynamic method calling only
	value.RegisterTypeMethods(l, TypeNameInstance,
		map[string]lua.LGFunction{
			"__index": instanceIndex, // Enables dynamic method access: instance:method_name()
		},
		nil,
	)

	// Create module table with top-level functions
	t := l.CreateTable(0, 3)
	t.RawSetString("get", l.NewFunction(m.getContract))
	t.RawSetString("find_implementations", l.NewFunction(m.findImplementations))
	t.RawSetString("is", l.NewFunction(m.is))

	// Make the table immutable so it can be safely reused
	t.Immutable = true

	m.moduleTable = t
}

// ================================
// BINDING ID PARSING
// ================================

// parseBindingArgs parses a binding ID with optional query parameters
// Format: "service:impl?key1=value1&key2=value2" or just "service:impl"
func parseBindingArgs(bindingID string) (string, map[string]any, error) {
	parts := strings.SplitN(bindingID, "?", 2)
	baseID := parts[0]

	if len(parts) == 1 {
		return baseID, make(map[string]any), nil // No query parameters
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("invalid query parameters in binding ID: %w", err)
	}

	// Convert url.Values to map[string]any with type conversion
	args := make(map[string]any)
	for k, v := range values {
		if len(v) > 0 {
			// Convert string values to appropriate types
			val := v[0] // Take first val if multiple

			// Try to convert to boolean
			if val == "true" {
				args[k] = true
			} else if val == "false" {
				args[k] = false
			} else if intVal, err := strconv.Atoi(val); err == nil {
				// Try to convert to integer
				args[k] = intVal
			} else if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
				// Try to convert to float
				args[k] = floatVal
			} else {
				// Keep as string
				args[k] = val
			}
		} else {
			// Empty value (like "flag=" or just "flag")
			args[k] = ""
		}
	}

	return baseID, args, nil
}

// ================================
// CALL CONTEXT UTILITIES (COMBINED SECURITY + APP CONTEXT)
// ================================

// newCallContext creates a call context from current Lua context
// Extracts existing application context values if present
func (m *Module) newCallContext(l *lua.LState) callContext {
	values := contextapi.NewContexter[any]()
	if ctxr, ok := l.Context().Value(contextapi.ValuesCtx).(*contextapi.Contexter[any]); ok {
		values = ctxr.Clone()
	}
	return callContext{values: values}
}

// clone creates a deep copy of call context for immutable chaining pattern
func (sc callContext) clone() callContext {
	return callContext{
		values:   sc.values.Clone(),
		actor:    sc.actor,
		hasActor: sc.hasActor,
		scope:    sc.scope,
		hasScope: sc.hasScope,
	}
}

// applyToContext applies both security and application context to base context
// This is the key method that combines all context types into the execution context
func (sc callContext) applyToContext(baseCtx context.Context) context.Context {
	ctx := baseCtx
	// Apply security context
	if sc.hasActor {
		ctx = secapi.WithActor(ctx, sc.actor)
	}
	if sc.hasScope {
		ctx = secapi.WithScope(ctx, sc.scope)
	}
	// Apply application context values
	if sc.values != nil {
		ctx = context.WithValue(ctx, contextapi.ValuesCtx, sc.values)
	}
	return ctx
}

// createUserData creates Lua userdata with proper metatable for type safety
func createUserData(l *lua.LState, v any, typeName string) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = v
	ud.Metatable = value.GetTypeMetatable(l, typeName)
	return ud
}

// ================================
// MODULE FUNCTIONS
// ================================

// getContract retrieves a contract definition and wraps it with empty call context
// This is the entry point that creates a Wrapper for chaining security context
func (m *Module) getContract(l *lua.LState) int {
	contractID := l.CheckString(1)

	// Security check for contract access permission
	if !security.IsAllowed(l.Context(), PermissionContractGet, contractID, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to access contract"))
		return 2
	}

	// Get required dependencies from context
	reg := contract.GetRegistry(l.Context())
	inst := contract.GetInstantiator(l.Context())
	if reg == nil || inst == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("contract registry or instantiator not found"))
		return 2
	}

	// Load contract definition from registry
	regID := registry.ParseID(contractID)
	contractDef, err := reg.GetContract(l.Context(), regID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create wrapper with empty call context (like funcs.new())
	wrapper := &Wrapper{
		definition:  contractDef,
		registry:    reg,
		inst:        inst,
		callContext: m.newCallContext(l),
	}

	l.Push(createUserData(l, wrapper, TypeNameContract))
	l.Push(lua.LNil)
	return 2
}

// findImplementations lists all binding IDs that implement the specified contract
func (m *Module) findImplementations(l *lua.LState) int {
	contractID := l.CheckString(1)

	// Security check for implementation listing permission
	if !security.IsAllowed(l.Context(), PermissionContractImplementations, contractID, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to list implementations"))
		return 2
	}

	reg := contract.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("contract registry not found"))
		return 2
	}

	regID := registry.ParseID(contractID)
	bindingIDs, err := reg.GetBindingsForContract(l.Context(), regID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Return array of binding ID strings
	result := l.CreateTable(len(bindingIDs), 0)
	for i, bindingID := range bindingIDs {
		result.RawSetInt(i+1, lua.LString(bindingID.String()))
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// is checks if an instance implements a specific contract ID
// Usage: contract.is(instance, "contract_id") returns true/false
func (m *Module) is(l *lua.LState) int {
	instance := l.CheckUserData(1)
	contractID := l.CheckString(2)

	// Validate instance is of correct type
	wrapper, ok := instance.Value.(*InstanceWrapper)
	if !ok {
		l.Push(lua.LBool(false))
		return 1
	}

	// Parse contract ID for comparison
	regID := registry.ParseID(contractID)

	// Check if instance implements the contract
	for _, contractDef := range wrapper.instance.Implements() {
		if contractDef.ID().String() == regID.String() {
			l.Push(lua.LBool(true))
			return 1
		}
	}

	l.Push(lua.LBool(false))
	return 1
}

// ================================
// CALL CONTEXT CHAIN METHODS (IMMUTABLE PATTERN LIKE FUNCS)
// ================================

// withContext creates a new wrapper with additional application context values
// Follows immutable chaining pattern: contract:with_context({key = value})
func (m *Module) withContext(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)

	// Security check for custom application context permission
	if !security.IsAllowed(l.Context(), PermissionContractAppContext, "context", nil) {
		l.RaiseError("not allowed to use contracts with custom context")
		return 0
	}

	ctxTable := l.CheckTable(2)
	newCallCtx := wrapper.callContext.clone()

	// Add new values from Lua table to existing context
	ctxTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			v := luaconv.ToGoAny(v)
			newCallCtx.values.SetValue(string(key), v)
		} else {
			l.ArgError(2, "context keys must be strings")
		}
	})

	// Create new wrapper with updated call context (immutable pattern)
	newWrapper := &Wrapper{
		definition:  wrapper.definition,
		registry:    wrapper.registry,
		inst:        wrapper.inst,
		callContext: newCallCtx,
	}

	l.Push(createUserData(l, newWrapper, TypeNameContract))
	return 1
}

// withActor creates a new wrapper with a specific security actor
// Follows immutable chaining pattern: contract:with_actor(actor)
func (m *Module) withActor(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)

	// Security check for custom security context permission
	if !security.IsAllowed(l.Context(), PermissionContractSecurityContext, "security", nil) {
		l.RaiseError("not allowed to use contracts with custom security context")
		return 0
	}

	// Validate actor parameter (cannot be nil for security)
	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "actor cannot be nil - security context cannot be removed")
		return 0
	}

	actor, ok := l.CheckUserData(2).Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	// Create new call context with actor
	newCallCtx := wrapper.callContext.clone()
	newCallCtx.actor = actor
	newCallCtx.hasActor = true

	// Create new wrapper with updated call context (immutable pattern)
	newWrapper := &Wrapper{
		definition:  wrapper.definition,
		registry:    wrapper.registry,
		inst:        wrapper.inst,
		callContext: newCallCtx,
	}

	l.Push(createUserData(l, newWrapper, TypeNameContract))
	return 1
}

// withScope creates a new wrapper with a specific security scope
// Follows immutable chaining pattern: contract:with_scope(scope)
func (m *Module) withScope(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)

	// Security check for custom security context permission
	if !security.IsAllowed(l.Context(), PermissionContractSecurityContext, "security", nil) {
		l.RaiseError("not allowed to use contracts with custom security context")
		return 0
	}

	// Validate scope parameter (cannot be nil for security)
	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "scope cannot be nil - security context cannot be removed")
		return 0
	}

	scope, ok := l.CheckUserData(2).Value.(secapi.Scope)
	if !ok {
		l.ArgError(2, "Args expected")
		return 0
	}

	// Create new call context with scope
	newCallCtx := wrapper.callContext.clone()
	newCallCtx.scope = scope
	newCallCtx.hasScope = true

	// Create new wrapper with updated call context (immutable pattern)
	newWrapper := &Wrapper{
		definition:  wrapper.definition,
		registry:    wrapper.registry,
		inst:        wrapper.inst,
		callContext: newCallCtx,
	}

	l.Push(createUserData(l, newWrapper, TypeNameContract))
	return 1
}

// ================================
// CONTRACT WRAPPER METHODS
// ================================

// contractOpen opens a binding with call context applied to create an instance
// The returned instance inherits the call context from the wrapper
// Now supports query parameters in binding ID: "service:impl?key=value&key2=value2"
// Also supports opening without binding ID to use default binding: contract:open()
func contractOpen(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)

	var bindingID string
	var queryArgs map[string]any
	var err error

	// Check if binding ID is provided
	if l.GetTop() >= 2 && l.Get(2).Type() != lua.LTNil {
		// Binding ID provided - parse it
		bindingIDArg := l.CheckString(2)
		baseID, parsedQueryArgs, parseErr := parseBindingArgs(bindingIDArg)
		if parseErr != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(parseErr.Error()))
			return 2
		}
		bindingID = baseID
		queryArgs = parsedQueryArgs
	} else {
		// No binding ID provided - use default binding
		defaultBindingID, err := wrapper.registry.GetDefaultBinding(l.Context(), wrapper.definition.ID())
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("no default binding available: %s", err.Error())))
			return 2
		}
		bindingID = defaultBindingID.String()
		queryArgs = make(map[string]any)
	}

	if !security.IsAllowed(l.Context(), PermissionContractBindingOpen, bindingID, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to open binding"))
		return 2
	}

	// Create merged call context: start with wrapper context
	mergedCallCtx := wrapper.callContext.clone()

	// Merge query parameters from binding ID (second priority)
	for k, v := range queryArgs {
		mergedCallCtx.values.SetValue(k, v)
	}

	// Merge additional context values from optional Lua table (highest priority)
	// These override/extend both chained context and query parameters
	// Check if context table is provided (can be argument 2 or 3 depending on whether binding ID was provided)
	contextArgIndex := 3
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTNil {
		// No binding ID provided, context table would be at index 2
		contextArgIndex = 2
	}

	if l.GetTop() >= contextArgIndex && l.Get(contextArgIndex).Type() == lua.LTTable {
		l.CheckTable(contextArgIndex).ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				mergedCallCtx.values.SetValue(string(kStr), luaconv.ToGoAny(v))
			}
		})
	}

	// Apply merged call context (security + app context) and instantiate binding
	ctx := mergedCallCtx.applyToContext(l.Context())
	regID := registry.ParseID(bindingID)

	// Get the binding to check what context keys are required
	binding, err := wrapper.registry.GetBinding(l.Context(), regID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Extract only required context keys from the merged context
	scope := make(registry.Metadata)
	if ctxr, ok := ctx.Value(contextapi.ValuesCtx).(*contextapi.Contexter[any]); ok {
		for _, boundContract := range binding.Contracts {
			for _, requiredKey := range boundContract.ContextRequired {
				if v, exists := ctxr.Value(requiredKey); exists {
					scope[requiredKey] = v
				}
			}
		}
	}

	instance, err := wrapper.inst.Instantiate(ctx, regID, scope)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Build method name -> contract ID mapping for fast method resolution
	owningContracts := make(map[string]string)
	for _, contractDef := range instance.Implements() {
		for _, method := range contractDef.Methods() {
			owningContracts[method.Name] = contractDef.ID().String()
		}
	}

	// Create instance wrapper with merged call context (includes query params + open() parameters)
	instWrapper := &InstanceWrapper{
		instance:        instance,
		owningContracts: owningContracts,
		callContext:     mergedCallCtx, // Use merged context, not original wrapper context
	}

	l.Push(createUserData(l, instWrapper, TypeNameInstance))
	l.Push(lua.LNil)
	return 2
}

// contractID returns the contract definition ID as a string
func contractID(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	l.Push(lua.LString(wrapper.definition.ID().String()))
	return 1
}

// contractMethods returns all methods defined in the contract with their schemas
func contractMethods(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	methods := wrapper.definition.Methods()
	methodsTable := l.CreateTable(len(methods), 0)

	for i, method := range methods {
		methodTable := l.CreateTable(0, 4)
		methodTable.RawSetString("name", lua.LString(method.Name))
		methodTable.RawSetString("description", lua.LString(method.Description))

		// Include input schemas array if present
		if len(method.InputSchemas) > 0 {
			methodTable.RawSetString("input_schemas", createSchemasTable(l, method.InputSchemas))
		}
		// Include output schemas array if present
		if len(method.OutputSchemas) > 0 {
			methodTable.RawSetString("output_schemas", createSchemasTable(l, method.OutputSchemas))
		}

		methodsTable.RawSetInt(i+1, methodTable)
	}

	l.Push(methodsTable)
	return 1
}

// contractMethod returns a specific method definition with its schemas
func contractMethod(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	methodName := l.CheckString(2)
	method, err := wrapper.definition.Method(methodName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	methodTable := l.CreateTable(0, 4)
	methodTable.RawSetString("name", lua.LString(method.Name))
	methodTable.RawSetString("description", lua.LString(method.Description))

	// Include input schemas array if present
	if len(method.InputSchemas) > 0 {
		methodTable.RawSetString("input_schemas", createSchemasTable(l, method.InputSchemas))
	}
	// Include output schemas array if present
	if len(method.OutputSchemas) > 0 {
		methodTable.RawSetString("output_schemas", createSchemasTable(l, method.OutputSchemas))
	}

	l.Push(methodTable)
	l.Push(lua.LNil)
	return 2
}

// contractImplementations returns all binding IDs that implement this contract
func contractImplementations(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	bindingIDs, err := wrapper.registry.GetBindingsForContract(l.Context(), wrapper.definition.ID())
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	result := l.CreateTable(len(bindingIDs), 0)
	for i, bindingID := range bindingIDs {
		result.RawSetInt(i+1, lua.LString(bindingID.String()))
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// ================================
// INSTANCE WRAPPER METHODS
// ================================

// instanceIndex provides dynamic method access with async support
// Enables syntax: instance:method_name() and instance:method_name_async()
func instanceIndex(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*InstanceWrapper)
	key := l.CheckString(2)

	// Parse method name and async flag from key
	isAsync := false
	methodName := key
	if len(key) > 6 && key[len(key)-6:] == "_async" {
		isAsync = true
		methodName = key[:len(key)-6]
	}

	// Check if method exists in any implemented contract
	contractID, exists := wrapper.owningContracts[methodName]
	if !exists {
		l.Push(lua.LNil)
		return 1
	}

	// Security check for method access permission
	if !security.IsAllowed(l.Context(), PermissionContractMethodAccess, methodName, registry.Metadata{"contract": contractID}) {
		l.Push(lua.LNil)
		return 1
	}

	// Return closure that will call the method with inherited call context
	l.Push(l.NewClosure(func(l *lua.LState) int {
		// Stack is already: instance, params
		// We need: instance, method_name, params
		// So just insert method name at position 2
		l.Insert(lua.LString(key), 2)

		return callInstanceMethod(l, isAsync)
	}))
	return 1
}

// ================================
// METHOD CALLING WITH INHERITED CALL CONTEXT
// ================================

// callInstanceMethod handles both sync and async method calls with inherited call context
// This is where the call context from the wrapper gets applied to method execution
func callInstanceMethod(l *lua.LState, isAsync bool) int {
	wrapper := l.CheckUserData(1).Value.(*InstanceWrapper)
	methodName := l.CheckString(2)

	// Remove _async suffix if present for method resolution
	if isAsync && len(methodName) > 6 && methodName[len(methodName)-6:] == "_async" {
		methodName = methodName[:len(methodName)-6]
	}

	// Validate method exists and security permissions
	contractID, exists := wrapper.owningContracts[methodName]
	if !exists {
		return handleError(l, isAsync, "method '%s' not found", methodName)
	}

	if !security.IsAllowed(l.Context(), PermissionContractMethodCall, methodName, registry.Metadata{"contract": contractID}) {
		return handleError(l, isAsync, "not allowed to call method")
	}

	// Collect method arguments with payload unwrapping support
	args := collectPayloadArgs(l, 3)

	// Get unit of work for execution context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		return handleError(l, isAsync, "no unit of work found")
	}

	// Execute method with appropriate sync/async pattern
	if isAsync {
		return executeAsync(l, wrapper, methodName, args, uw)
	} else {
		return executeSync(l, wrapper, methodName, args, uw)
	}
}

// handleError centralizes error handling for sync vs async method calls
func handleError(l *lua.LState, isAsync bool, format string, args ...interface{}) int {
	errMsg := fmt.Sprintf(format, args...)
	if isAsync {
		l.RaiseError("%s", errMsg) // Async: raise error immediately
		return 0
	}
	// Sync: return nil, error
	l.Push(lua.LNil)
	l.Push(lua.LString(errMsg))
	return 2
}

// collectPayloadArgs collects method arguments from Lua stack with payload unwrapping
// Supports both regular Lua values and payload wrapper userdata
func collectPayloadArgs(l *lua.LState, startIndex int) []payload.Payload {
	var args []payload.Payload
	for i := startIndex; i <= l.GetTop(); i++ {
		v := l.Get(i)
		// Check for payload wrapper userdata (avoid double-wrapping)
		if ud, ok := v.(*lua.LUserData); ok {
			if pw, ok := ud.Value.(*payloadmod.Wrapper); ok {
				args = append(args, pw.Payload)
				continue
			}
		}
		// Regular Lua value - export to payload
		args = append(args, luaconv.ExportPayload(v))
	}
	return args
}

// executeAsync handles async method execution with inherited call context
// Returns a command object for async operation management
func executeAsync(l *lua.LState, wrapper *InstanceWrapper, methodName string, args []payload.Payload, uw engine.UnitOfWork) int {
	// Detach from unit of work and apply inherited call context
	baseCtx := engine.DetachUnitOfWork(uw.Context())
	execCtx := wrapper.callContext.applyToContext(baseCtx)
	ctx, cancel := context.WithCancel(execCtx)
	cmd := command.NewCommand(l, methodName, func(_ runtime.Command) { cancel() }, args...)

	// Execute method in background with call context applied
	uw.Run(func(work engine.UnitOfWork) {
		resultChan, err := wrapper.instance.Call(ctx, methodName, args)
		if err != nil {
			_ = cmd.Complete(&runtime.Result{Error: err})
			return
		}

		select {
		case result := <-resultChan:
			_ = cmd.Complete(result)
		case <-work.Context().Done():
			_ = cmd.Cancel()
		}
	})

	l.Push(command.WrapCommand(l, cmd))
	return 1
}

// executeSync handles sync method execution with inherited call context
// Uses coroutine wrapping for non-blocking execution
func executeSync(l *lua.LState, wrapper *InstanceWrapper, methodName string, args []payload.Payload, uw engine.UnitOfWork) int {
	coroutine.Wrap(l, func() *engine.Update {
		// Detach from unit of work and apply inherited call context
		baseCtx := engine.DetachUnitOfWork(uw.Context())
		execCtx := wrapper.callContext.applyToContext(baseCtx)

		resultChan, err := wrapper.instance.Call(execCtx, methodName, args)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		select {
		case result := <-resultChan:
			if result.Error != nil {
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(result.Error.Error())}, nil)
			}

			// Transcode result to Lua format if present
			if result.Value != nil {
				if dtt := payload.GetTranscoder(uw.Context()); dtt != nil {
					if luaResult, err := dtt.Transcode(result.Value, payload.Lua); err == nil {
						return engine.NewUpdate(nil, []lua.LValue{luaResult.Data().(lua.LValue), lua.LNil}, nil)
					}
				}
			}

			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)

		case <-uw.Context().Done():
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("canceled")}, nil)
		}
	})

	return -1 // Yield for coroutine
}

// ================================
// SCHEMA UTILITIES
// ================================

// createSchemaTable converts a contract.SchemaDefinition to a Lua table
// Includes both format and definition for complete schema information
func createSchemaTable(l *lua.LState, schema contract.SchemaDefinition) *lua.LTable {
	schemaTable := l.CreateTable(0, 2)
	schemaTable.RawSetString("format", lua.LString(schema.Format))

	// Convert schema definition to Lua value if present
	if schema.Definition != nil {
		if luaVal, err := luaconv.GoToLua(schema.Definition); err == nil {
			schemaTable.RawSetString("definition", luaVal)
		}
	}

	return schemaTable
}

// createSchemasTable converts an array of contract.SchemaDefinition to a Lua table array
// Used for input_schemas and output_schemas arrays in method definitions
func createSchemasTable(l *lua.LState, schemas []contract.SchemaDefinition) *lua.LTable {
	schemasTable := l.CreateTable(len(schemas), 0)
	for i, schema := range schemas {
		schemasTable.RawSetInt(i+1, createSchemaTable(l, schema))
	}
	return schemasTable
}
