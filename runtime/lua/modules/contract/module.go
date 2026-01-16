package contract

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/attrs"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const (
	contractTypeName = "contract.Contract"
	instanceTypeName = "contract.Instance"
)

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

func init() {
	// Register contract type methods
	value.RegisterTypeMethods(nil, contractTypeName, nil, map[string]lua.LGoFunc{
		"id":              contractID,
		"methods":         contractMethods,
		"method":          contractMethod,
		"implementations": contractImplementations,
		"open":            contractOpen,
		"with_context":    contractWithContext,
		"with_actor":      contractWithActor,
		"with_scope":      contractWithScope,
	})

	// Register instance type with dynamic method access via __index
	value.RegisterTypeMethods(nil, instanceTypeName,
		map[string]lua.LGoFunc{
			"__index": instanceIndex,
		},
		nil,
	)

	// Set cancel function for Future type
	future.CancelFunc = futureCancelImpl

	moduleTable = lua.CreateTable(0, 4)
	moduleTable.RawSetString("get", lua.LGoFunc(getContract))
	moduleTable.RawSetString("open", lua.LGoFunc(openBinding))
	moduleTable.RawSetString("find_implementations", lua.LGoFunc(findImplementations))
	moduleTable.RawSetString("is", lua.LGoFunc(isContract))
	moduleTable.Immutable = true

	yieldTypes = []luaapi.YieldType{
		{Sample: &OpenYield{}, CmdID: contract.Open},
		{Sample: &CallYield{}, CmdID: contract.Call},
		{Sample: &AsyncCallYield{}, CmdID: contract.AsyncCall},
		{Sample: &AsyncCancelYield{}, CmdID: contract.AsyncCancel},
	}
}

// Module is the contract module definition.
var Module = &luaapi.ModuleDef{
	Name:        "contract",
	Description: "Contract-based interface invocation",
	Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, yieldTypes
	},
	Types: ModuleTypes,
}

// Wrapper holds a contract definition with optional context for introspection.
type Wrapper struct {
	definition contract.Contract
	registry   contract.Registry
	values     contextapi.Values
	actor      secapi.Actor
	hasActor   bool
	scope      secapi.Scope
	hasScope   bool
}

// InstanceWrapper holds an opened contract instance.
type InstanceWrapper struct {
	instance contract.Instance
}

// parseBindingID parses a binding ID with optional query parameters.
// Format: "service:impl?key1=value1&key2=value2" or just "service:impl"
func parseBindingID(bindingID string) (string, map[string]any, error) {
	parts := strings.SplitN(bindingID, "?", 2)
	baseID := parts[0]

	if len(parts) == 1 {
		return baseID, nil, nil
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", nil, err
	}

	args := make(map[string]any)
	for k, v := range values {
		if len(v) == 0 {
			args[k] = ""
			continue
		}
		val := v[0]
		if val == "true" {
			args[k] = true
		} else if val == "false" {
			args[k] = false
		} else if intVal, err := strconv.Atoi(val); err == nil {
			args[k] = intVal
		} else if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			args[k] = floatVal
		} else {
			args[k] = val
		}
	}

	return baseID, args, nil
}

// getContract retrieves a contract definition by ID.
func getContract(l *lua.LState) int {
	contractID := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "contract.get", contractID, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to access contract: "+contractID).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	reg := contract.GetRegistry(l.Context())
	if reg == nil {
		luaErr := lua.NewLuaError(l, "contract registry not found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	regID := registry.ParseID(contractID)
	def, err := reg.GetContract(l.Context(), regID)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "get contract failed")
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	wrapper := &Wrapper{
		definition: def,
		registry:   reg,
	}

	l.Push(value.NewTypedUserData(l, wrapper, contractTypeName))
	l.Push(lua.LNil)
	return 2
}

// openBinding opens a binding directly by ID.
// Supports query parameters: "service:impl?key=value"
func openBinding(l *lua.LState) int {
	bindingIDArg := l.CheckString(1)

	baseID, queryArgs, err := parseBindingID(bindingIDArg)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "parse binding ID failed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	if !security.IsAllowed(l.Context(), "contract.open", baseID, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to open binding: "+baseID).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Build scope from query parameters and optional table
	var scope attrs.Bag
	if queryArgs != nil || (l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable) {
		scope = attrs.NewBag()
		for k, v := range queryArgs {
			scope.Set(k, v)
		}
		if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
			l.CheckTable(2).ForEach(func(k, v lua.LValue) {
				if key, ok := k.(lua.LString); ok {
					scope.Set(string(key), value.ToGoAny(v))
				}
			})
		}
	}

	yield := AcquireOpenYield()
	yield.BindingID = registry.ParseID(baseID)
	yield.Scope = scope

	l.Push(yield)
	return -1
}

// isContract checks if an instance implements a specific contract.
func isContract(l *lua.LState) int {
	ud := l.CheckUserData(1)
	contractID := l.CheckString(2)

	wrapper, ok := ud.Value.(*InstanceWrapper)
	if !ok {
		l.Push(lua.LBool(false))
		return 1
	}

	regID := registry.ParseID(contractID)
	for _, c := range wrapper.instance.Implements() {
		cID := c.ID()
		if cID.String() == regID.String() {
			l.Push(lua.LBool(true))
			return 1
		}
	}

	l.Push(lua.LBool(false))
	return 1
}

// findImplementations lists all binding IDs that implement a contract.
func findImplementations(l *lua.LState) int {
	contractID := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "contract.implementations", contractID, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to list implementations: "+contractID).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	reg := contract.GetRegistry(l.Context())
	if reg == nil {
		luaErr := lua.NewLuaError(l, "contract registry not found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	regID := registry.ParseID(contractID)
	bindings, err := reg.GetBindingsForContract(l.Context(), regID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get implementations failed"))
		return 2
	}

	result := l.CreateTable(len(bindings), 0)
	for i, bindingID := range bindings {
		result.RawSetInt(i+1, lua.LString(bindingID.String()))
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// Contract wrapper methods

func contractID(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	id := wrapper.definition.ID()
	l.Push(lua.LString(id.String()))
	return 1
}

func contractMethods(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	methods := wrapper.definition.Methods()
	result := l.CreateTable(len(methods), 0)

	for i, method := range methods {
		methodTable := l.CreateTable(0, 4)
		methodTable.RawSetString("name", lua.LString(method.Name))
		methodTable.RawSetString("description", lua.LString(method.Description))
		if len(method.InputSchemas) > 0 {
			methodTable.RawSetString("input_schemas", createSchemasTable(l, method.InputSchemas))
		}
		if len(method.OutputSchemas) > 0 {
			methodTable.RawSetString("output_schemas", createSchemasTable(l, method.OutputSchemas))
		}
		result.RawSetInt(i+1, methodTable)
	}

	l.Push(result)
	return 1
}

func contractMethod(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)
	methodName := l.CheckString(2)

	method, err := wrapper.definition.Method(methodName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get method failed"))
		return 2
	}

	methodTable := l.CreateTable(0, 4)
	methodTable.RawSetString("name", lua.LString(method.Name))
	methodTable.RawSetString("description", lua.LString(method.Description))
	if len(method.InputSchemas) > 0 {
		methodTable.RawSetString("input_schemas", createSchemasTable(l, method.InputSchemas))
	}
	if len(method.OutputSchemas) > 0 {
		methodTable.RawSetString("output_schemas", createSchemasTable(l, method.OutputSchemas))
	}

	l.Push(methodTable)
	l.Push(lua.LNil)
	return 2
}

func contractImplementations(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)

	bindings, err := wrapper.registry.GetBindingsForContract(l.Context(), wrapper.definition.ID())
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get implementations failed"))
		return 2
	}

	result := l.CreateTable(len(bindings), 0)
	for i, bindingID := range bindings {
		result.RawSetInt(i+1, lua.LString(bindingID.String()))
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

func contractOpen(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*Wrapper)

	var bindingID string
	var queryArgs map[string]any

	// Check if binding ID is provided
	if l.GetTop() >= 2 && l.Get(2).Type() != lua.LTNil {
		bindingIDArg := l.CheckString(2)
		baseID, args, err := parseBindingID(bindingIDArg)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "parse binding ID failed"))
			return 2
		}
		bindingID = baseID
		queryArgs = args
	} else {
		// Use default binding
		defaultID, err := wrapper.registry.GetDefaultBinding(l.Context(), wrapper.definition.ID())
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "no default binding"))
			return 2
		}
		bindingID = defaultID.String()
	}

	if !security.IsAllowed(l.Context(), "contract.open", bindingID, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to open binding: "+bindingID).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Build scope: query args -> wrapper context -> explicit table (highest priority)
	var scope attrs.Bag
	hasScope := queryArgs != nil || wrapper.values != nil

	scopeIndex := 3
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTNil {
		scopeIndex = 2
	}
	if l.GetTop() >= scopeIndex && l.Get(scopeIndex).Type() == lua.LTTable {
		hasScope = true
	}

	if hasScope {
		scope = attrs.NewBag()
		// Query args first (lowest priority)
		for k, v := range queryArgs {
			scope.Set(k, v)
		}
		// Wrapper context values
		if wrapper.values != nil {
			wrapper.values.Iterate(func(key string, val any) {
				scope.Set(key, val)
			})
		}
		// Explicit table (highest priority)
		if l.GetTop() >= scopeIndex && l.Get(scopeIndex).Type() == lua.LTTable {
			l.CheckTable(scopeIndex).ForEach(func(k, v lua.LValue) {
				if key, ok := k.(lua.LString); ok {
					scope.Set(string(key), value.ToGoAny(v))
				}
			})
		}
	}

	yield := AcquireOpenYield()
	yield.BindingID = registry.ParseID(bindingID)
	yield.Scope = scope
	yield.Values = wrapper.values
	yield.Actor = wrapper.actor
	yield.HasActor = wrapper.hasActor
	yield.SecurityScope = wrapper.scope
	yield.HasScope = wrapper.hasScope

	l.Push(yield)
	return -1
}

func contractWithContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*Wrapper)
	if !ok {
		l.ArgError(1, "Contract expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "contract.context", "context", nil) {
		luaErr := lua.NewLuaError(l, "not allowed to use contracts with custom context").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	ctxTable := l.CheckTable(2)

	newValues := contextapi.NewValues()
	if wrapper.values != nil {
		wrapper.values.Iterate(func(key string, val any) {
			newValues.Set(key, val)
		})
	}

	ctxTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			newValues.Set(string(key), value.ToGoAny(v))
		}
	})

	newWrapper := &Wrapper{
		definition: wrapper.definition,
		registry:   wrapper.registry,
		values:     newValues,
		actor:      wrapper.actor,
		hasActor:   wrapper.hasActor,
		scope:      wrapper.scope,
		hasScope:   wrapper.hasScope,
	}

	value.PushTypedUserData(l, newWrapper, contractTypeName)
	return 1
}

func contractWithActor(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*Wrapper)
	if !ok {
		l.ArgError(1, "Contract expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "contract.security", "security", nil) {
		luaErr := lua.NewLuaError(l, "not allowed to use contracts with custom security context").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "actor cannot be nil")
		return 0
	}

	actorUD := l.CheckUserData(2)
	actor, ok := actorUD.Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	newWrapper := &Wrapper{
		definition: wrapper.definition,
		registry:   wrapper.registry,
		values:     wrapper.values,
		actor:      actor,
		hasActor:   true,
		scope:      wrapper.scope,
		hasScope:   wrapper.hasScope,
	}

	value.PushTypedUserData(l, newWrapper, contractTypeName)
	return 1
}

func contractWithScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*Wrapper)
	if !ok {
		l.ArgError(1, "Contract expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "contract.security", "security", nil) {
		luaErr := lua.NewLuaError(l, "not allowed to use contracts with custom security context").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "scope cannot be nil")
		return 0
	}

	scopeUD := l.CheckUserData(2)
	scope, ok := scopeUD.Value.(secapi.Scope)
	if !ok {
		l.ArgError(2, "Scope expected")
		return 0
	}

	newWrapper := &Wrapper{
		definition: wrapper.definition,
		registry:   wrapper.registry,
		values:     wrapper.values,
		actor:      wrapper.actor,
		hasActor:   wrapper.hasActor,
		scope:      scope,
		hasScope:   true,
	}

	value.PushTypedUserData(l, newWrapper, contractTypeName)
	return 1
}

// instanceIndex provides dynamic method access via __index metamethod.
// Enables syntax: instance:method_name() and instance:method_name_async()
func instanceIndex(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*InstanceWrapper)
	key := l.CheckString(2)

	// Check for _async suffix
	isAsync := false
	methodName := key
	if len(key) > 6 && key[len(key)-6:] == "_async" {
		isAsync = true
		methodName = key[:len(key)-6]
	}

	// Verify method exists in implemented contracts
	found := false
	for _, c := range wrapper.instance.Implements() {
		if _, err := c.Method(methodName); err == nil {
			found = true
			break
		}
	}

	if !found {
		l.Push(lua.LNil)
		return 1
	}

	// Return closure that calls the method
	l.Push(l.NewClosure(func(l *lua.LState) int {
		return callMethod(l, wrapper, methodName, isAsync)
	}))
	return 1
}

func callMethod(l *lua.LState, wrapper *InstanceWrapper, method string, isAsync bool) int {
	if !security.IsAllowed(l.Context(), "contract.call", method, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to call method: "+method).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Collect arguments (skip self at position 1)
	var args payload.Payloads
	for i := 2; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ExportPayload(l.Get(i)))
	}

	if isAsync {
		return callMethodAsync(l, wrapper, method, args)
	}
	return callMethodSync(l, wrapper, method, args)
}

func callMethodSync(l *lua.LState, wrapper *InstanceWrapper, method string, args payload.Payloads) int {
	yield := AcquireCallYield()
	yield.Instance = wrapper.instance
	yield.Method = method
	yield.Args = args

	l.Push(yield)
	return -1
}

func callMethodAsync(l *lua.LState, wrapper *InstanceWrapper, method string, args payload.Payloads) int {
	proc := engine.GetProcess(l)
	if proc == nil {
		luaErr := lua.NewLuaError(l, "no process context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	topic := "@future:" + uuid.New().String()
	ch, subErr := proc.Subscribe(topic, 1)
	if subErr != nil {
		luaErr := lua.WrapErrorWithLua(l, subErr, "subscribe failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	f := future.New(topic, ch)
	proc.SetTopicHandler(topic, f.CreateHandler())

	yield := AcquireAsyncCallYield()
	yield.Instance = wrapper.instance
	yield.Method = method
	yield.Args = args
	yield.Topic = topic
	yield.Future = f

	l.Push(yield)
	return -1
}

func futureCancelImpl(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*future.Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	yield := AcquireAsyncCancelYield()
	yield.Topic = f.Topic
	l.Push(yield)
	return -1
}

// Schema helpers

func createSchemasTable(l *lua.LState, schemas []contract.SchemaDefinition) *lua.LTable {
	result := l.CreateTable(len(schemas), 0)
	for i, schema := range schemas {
		schemaTable := l.CreateTable(0, 2)
		schemaTable.RawSetString("format", lua.LString(schema.Format))
		if schema.Definition != nil {
			if lv, err := luaconv.GoToLua(schema.Definition); err == nil {
				schemaTable.RawSetString("definition", lv)
			}
		}
		result.RawSetInt(i+1, schemaTable)
	}
	return result
}
