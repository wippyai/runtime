package contract

import (
	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
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
	})

	// Register instance type with dynamic method access
	value.RegisterTypeMethods(nil, instanceTypeName,
		map[string]lua.LGoFunc{
			"__index": instanceIndex,
		},
		map[string]lua.LGoFunc{
			"id":         instanceID,
			"implements": instanceImplements,
		},
	)

	// Set cancel function for Future type
	future.CancelFunc = futureCancelImpl

	moduleTable = lua.CreateTable(0, 3)
	moduleTable.RawSetString("get", lua.LGoFunc(getContract))
	moduleTable.RawSetString("open", lua.LGoFunc(openBinding))
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
}

// ContractWrapper holds a contract definition for introspection.
type ContractWrapper struct {
	definition contract.Contract
	registry   contract.Registry
}

// InstanceWrapper holds an opened contract instance.
type InstanceWrapper struct {
	instance contract.Instance
}

// getContract retrieves a contract definition by ID.
func getContract(l *lua.LState) int {
	contractID := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "contract.get", contractID, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to access contract: "+contractID).
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	reg := contract.GetRegistry(l.Context())
	if reg == nil {
		luaErr := lua.NewLuaError(l, "contract registry not found").
			WithKind(lua.KindInternal).
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

	wrapper := &ContractWrapper{
		definition: def,
		registry:   reg,
	}

	l.Push(value.NewTypedUserData(l, wrapper, contractTypeName))
	l.Push(lua.LNil)
	return 2
}

// openBinding opens a binding directly by ID.
func openBinding(l *lua.LState) int {
	bindingID := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "contract.open", bindingID, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to open binding: "+bindingID).
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Parse optional scope table
	var scope attrs.Bag
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		scope = attrs.NewBag()
		l.CheckTable(2).ForEach(func(k, v lua.LValue) {
			if key, ok := k.(lua.LString); ok {
				scope.Set(string(key), value.ToGoAny(v))
			}
		})
	}

	yield := AcquireOpenYield()
	yield.BindingID = registry.ParseID(bindingID)
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
		if c.ID().String() == regID.String() {
			l.Push(lua.LBool(true))
			return 1
		}
	}

	l.Push(lua.LBool(false))
	return 1
}

// Contract wrapper methods

func contractID(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*ContractWrapper)
	l.Push(lua.LString(wrapper.definition.ID().String()))
	return 1
}

func contractMethods(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*ContractWrapper)
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
	wrapper := l.CheckUserData(1).Value.(*ContractWrapper)
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
	wrapper := l.CheckUserData(1).Value.(*ContractWrapper)

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
	wrapper := l.CheckUserData(1).Value.(*ContractWrapper)

	var bindingID string
	var scope attrs.Bag

	// Check if binding ID is provided
	if l.GetTop() >= 2 && l.Get(2).Type() != lua.LTNil {
		bindingID = l.CheckString(2)
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
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Parse optional scope table
	scopeIndex := 3
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTNil {
		scopeIndex = 2
	}
	if l.GetTop() >= scopeIndex && l.Get(scopeIndex).Type() == lua.LTTable {
		scope = attrs.NewBag()
		l.CheckTable(scopeIndex).ForEach(func(k, v lua.LValue) {
			if key, ok := k.(lua.LString); ok {
				scope.Set(string(key), value.ToGoAny(v))
			}
		})
	}

	yield := AcquireOpenYield()
	yield.BindingID = registry.ParseID(bindingID)
	yield.Scope = scope

	l.Push(yield)
	return -1
}

// Instance wrapper methods

func instanceID(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*InstanceWrapper)
	l.Push(lua.LString(wrapper.instance.ID().String()))
	return 1
}

func instanceImplements(l *lua.LState) int {
	wrapper := l.CheckUserData(1).Value.(*InstanceWrapper)
	contracts := wrapper.instance.Implements()
	result := l.CreateTable(len(contracts), 0)

	for i, c := range contracts {
		result.RawSetInt(i+1, lua.LString(c.ID().String()))
	}

	l.Push(result)
	return 1
}

// instanceIndex provides dynamic method access via __index metamethod.
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

	// Verify method exists
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
			WithKind(lua.KindPermissionDenied).
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
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	topic := "@future:" + uuid.New().String()
	ch := engine.NewChannel(1)

	if err := proc.Subscribe(topic, ch); err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "subscribe failed").
			WithKind(lua.KindInternal).
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
