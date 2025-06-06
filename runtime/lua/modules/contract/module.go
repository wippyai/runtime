package contract

import (
	"fmt"

	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/security"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

/*
Contract Module - Two-Level Security Implementation

This module implements a two-level security check system for contract method calls:

1. Contract-level check: "contract.call" action with contract ID and call metadata
2. Method-level check: "contract.method.call" action with method name and call metadata

The call metadata includes:
- binding_id: The binding being used
- method_name: The method being called
- scope.*: All scope variables prefixed with "scope."
- contracts: Array of contract IDs this instance implements
- arg_count: Number of arguments passed

Security policies can use the metadata helpers to make decisions:
- meta.StringValue("binding_id")
- meta.TagValue("contracts")
- meta.IntValue("arg_count")
- meta.BoolValue("scope.admin")

Example usage in Lua:
```lua
-- Load contract and open binding with scope
local kb = contract("contracts:embeddings_service")
local instance = kb:open("embeddings:partitioned_kb", {
    partition_id = "user_123",
    admin = false
})

-- This triggers both security checks:
-- 1. contract.call on "contracts:embeddings_service"
-- 2. contract.method.call on "embed"
local result = instance:embed({text = "Hello"})
```
*/

const (
	moduleName        = "contract"
	contractMetatable = "contract.Contract"
	instanceMetatable = "contract.Instance"
)

// Module represents the contract module for Lua
type Module struct {
	log *zap.Logger
}

// ContractWrapper wraps a contract definition for Lua
type ContractWrapper struct {
	contractDef contract.Contract
	registry    contract.Registry
	inst        contract.Instantiator
	log         *zap.Logger
}

// InstanceWrapper wraps a contract instance for Lua
type InstanceWrapper struct {
	instance contract.Instance
	log      *zap.Logger
}

// NewContractModule creates a new contract module
func NewContractModule(log *zap.Logger) *Module {
	return &Module{
		log: log.Named("contract"),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return moduleName
}

// Loader loads the module into the Lua state
func (m *Module) Loader(l *lua.LState) int {
	// Register types
	m.registerContractType(l)
	m.registerInstanceType(l)

	// Create module table
	mod := l.CreateTable(0, 1)
	mod.RawSetString("get", l.NewFunction(m.getContract))

	// Set global contract function
	l.SetGlobal("contract", l.NewFunction(m.contractGlobal))

	l.Push(mod)
	return 1
}

// registerContractType registers the Contract type and methods
func (m *Module) registerContractType(l *lua.LState) {
	value.RegisterMethods(l, contractMetatable, map[string]lua.LGFunction{
		"open":    contractOpen,
		"methods": contractMethods,
		"method":  contractMethod,
		"id":      contractID,
	})
}

// registerInstanceType registers the Instance type and methods
func (m *Module) registerInstanceType(l *lua.LState) {
	value.RegisterMethods(l, instanceMetatable, map[string]lua.LGFunction{
		"id":         instanceID,
		"scope":      instanceScope,
		"implements": instanceImplements,
		"call":       instanceCall,
	})

	// Register metamethods for dynamic method calling
	value.RegisterTypeMethods(l, instanceMetatable, map[string]lua.LGFunction{
		"__index": instanceIndex,
	}, nil)
}

// contractGlobal is the global contract() function
func (m *Module) contractGlobal(l *lua.LState) int {
	return m.getContract(l)
}

// getContract loads a contract definition by ID
func (m *Module) getContract(l *lua.LState) int {
	// Get contract ID
	contractID := l.CheckString(1)
	if contractID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("contract ID is required"))
		return 2
	}

	// Parse registry ID
	regID := registry.ParseID(contractID)

	// Build metadata for security check
	contractMeta := make(registry.Metadata)
	contractMeta["contract_id"] = contractID
	contractMeta["namespace"] = regID.NS
	contractMeta["name"] = regID.Name

	// Security check
	if !security.IsAllowed(l.Context(), "contract.get", contractID, contractMeta) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to access contract: %s", contractID)))
		return 2
	}

	// Get contract services from context
	reg := contract.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("contract registry not found in context"))
		return 2
	}

	inst := contract.GetInstantiator(l.Context())
	if inst == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("contract instantiator not found in context"))
		return 2
	}

	// Load contract
	contractObj, err := reg.GetContract(l.Context(), regID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create wrapper
	wrapper := &ContractWrapper{
		contractDef: contractObj,
		registry:    reg,
		inst:        inst,
		log:         m.log,
	}

	// Create userdata
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, contractMetatable)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// contractOpen opens a contract binding with optional scope
func contractOpen(l *lua.LState) int {
	// Get contract wrapper
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*ContractWrapper)
	if !ok {
		l.ArgError(1, "contract expected")
		return 0
	}

	// Get binding ID
	bindingID := l.CheckString(2)
	if bindingID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("binding ID is required"))
		return 2
	}

	// Parse binding registry ID
	regID := registry.ParseID(bindingID)

	// Get optional scope metadata
	var scope registry.Metadata
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTTable {
		scopeTable := l.CheckTable(3)
		scope = make(registry.Metadata)
		scopeTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				scope[string(kStr)] = luaconv.ToGoAny(v)
			}
		})
	}

	// Build metadata for security check
	bindingMeta := make(registry.Metadata)
	bindingMeta["binding_id"] = bindingID
	bindingMeta["contract_id"] = wrapper.contractDef.ID().String()
	bindingMeta["namespace"] = regID.NS
	bindingMeta["name"] = regID.Name

	// Add scope information to metadata
	if scope != nil {
		for k, v := range scope {
			bindingMeta["scope."+k] = v
		}
	}

	// Security check for binding access
	if !security.IsAllowed(l.Context(), "contract.binding.open", bindingID, bindingMeta) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to open binding: %s", bindingID)))
		return 2
	}

	// Instantiate the contract binding
	instance, err := wrapper.inst.Instantiate(l.Context(), regID, scope)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create instance wrapper
	instWrapper := &InstanceWrapper{
		instance: instance,
		log:      wrapper.log,
	}

	// Create userdata
	instUD := l.NewUserData()
	instUD.Value = instWrapper
	instUD.Metatable = value.GetTypeMetatable(l, instanceMetatable)

	l.Push(instUD)
	l.Push(lua.LNil)
	return 2
}

// contractMethods returns all methods in the contract
func contractMethods(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*ContractWrapper)
	if !ok {
		l.ArgError(1, "contract expected")
		return 0
	}

	methods := wrapper.contractDef.Methods()
	methodsTable := l.CreateTable(len(methods), 0)

	for i, method := range methods {
		methodTable := l.CreateTable(0, 4)
		methodTable.RawSetString("name", lua.LString(method.Name))
		methodTable.RawSetString("description", lua.LString(method.Description))

		// Add schema info if available
		if method.InputSchema.Format != "" {
			inputTable := l.CreateTable(0, 2)
			inputTable.RawSetString("format", lua.LString(method.InputSchema.Format))
			methodTable.RawSetString("input_schema", inputTable)
		}

		if method.OutputSchema.Format != "" {
			outputTable := l.CreateTable(0, 2)
			outputTable.RawSetString("format", lua.LString(method.OutputSchema.Format))
			methodTable.RawSetString("output_schema", outputTable)
		}

		methodsTable.RawSetInt(i+1, methodTable)
	}

	l.Push(methodsTable)
	return 1
}

// contractMethod returns a specific method definition
func contractMethod(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*ContractWrapper)
	if !ok {
		l.ArgError(1, "contract expected")
		return 0
	}

	methodName := l.CheckString(2)
	method, err := wrapper.contractDef.Method(methodName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	methodTable := l.CreateTable(0, 4)
	methodTable.RawSetString("name", lua.LString(method.Name))
	methodTable.RawSetString("description", lua.LString(method.Description))

	l.Push(methodTable)
	l.Push(lua.LNil)
	return 2
}

// contractID returns the contract ID
func contractID(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*ContractWrapper)
	if !ok {
		l.ArgError(1, "contract expected")
		return 0
	}

	l.Push(lua.LString(wrapper.contractDef.ID().String()))
	return 1
}

// instanceID returns the instance binding ID
func instanceID(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*InstanceWrapper)
	if !ok {
		l.ArgError(1, "instance expected")
		return 0
	}

	l.Push(lua.LString(wrapper.instance.ID().String()))
	return 1
}

// instanceScope returns the instance scope
func instanceScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*InstanceWrapper)
	if !ok {
		l.ArgError(1, "instance expected")
		return 0
	}

	scope := wrapper.instance.Scope()
	if scope == nil {
		l.Push(lua.LNil)
		return 1
	}

	scopeTable := l.CreateTable(0, len(scope))
	for k, v := range scope {
		val, err := luaconv.GoToLua(v)
		if err != nil {
			continue // Skip unconvertible values
		}
		scopeTable.RawSetString(k, val)
	}

	l.Push(scopeTable)
	return 1
}

// instanceImplements returns the contracts this instance implements
func instanceImplements(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*InstanceWrapper)
	if !ok {
		l.ArgError(1, "instance expected")
		return 0
	}

	implementedContracts := wrapper.instance.Implements()
	contractsTable := l.CreateTable(len(implementedContracts), 0)

	for i, contractDef := range implementedContracts {
		contractsTable.RawSetInt(i+1, lua.LString(contractDef.ID().String()))
	}

	l.Push(contractsTable)
	return 1
}

// instanceCall calls a method on the instance
func instanceCall(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*InstanceWrapper)
	if !ok {
		l.ArgError(1, "instance expected")
		return 0
	}

	methodName := l.CheckString(2)

	// Build call context metadata
	callMeta := make(registry.Metadata)
	callMeta["binding_id"] = wrapper.instance.ID().String()
	callMeta["method_name"] = methodName

	// Add instance scope to call metadata
	if scope := wrapper.instance.Scope(); scope != nil {
		for k, v := range scope {
			callMeta["scope."+k] = v
		}
	}

	// Add contracts this instance implements
	implementedContracts := wrapper.instance.Implements()
	contractIDs := make([]string, len(implementedContracts))
	for i, contractDef := range implementedContracts {
		contractIDs[i] = contractDef.ID().String()
	}
	callMeta["contracts"] = contractIDs

	// First: Contract-level security check
	for _, contractDef := range implementedContracts {
		if !security.IsAllowed(l.Context(), "contract.call", contractDef.ID().String(), callMeta) {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("not allowed to call contract: %s", contractDef.ID().String())))
			return 2
		}
	}

	// Collect arguments
	var args []payload.Payload
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ExportPayload(l.Get(i)))
	}

	// Add argument count to metadata for further security checks
	callMeta["arg_count"] = len(args)

	// Second: Method-level security check
	if !security.IsAllowed(l.Context(), "contract.method.call", methodName, callMeta) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to call method: %s", methodName)))
		return 2
	}

	// Get unit of work context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work context found"))
		return 2
	}

	// Wrap in coroutine for execution
	coroutine.Wrap(l, func() *engine.Update {
		// Call the method
		resultChan, err := wrapper.instance.Call(uw.Context(), methodName, args)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Wait for result
		select {
		case result := <-resultChan:
			if result.Error != nil {
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(result.Error.Error())}, nil)
			}

			if result.Value != nil {
				dtt := payload.GetTranscoder(uw.Context())
				if dtt == nil {
					return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("transcoder not found")}, nil)
				}

				luaResult, err := dtt.Transcode(result.Value, payload.Lua)
				if err != nil {
					return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
				}

				return engine.NewUpdate(nil, []lua.LValue{luaResult.Data().(lua.LValue), lua.LNil}, nil)
			}

			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)

		case <-uw.Context().Done():
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("execution canceled")}, nil)
		}
	})

	return -1 // Yield for coroutine
}

// instanceIndex implements dynamic method calling via __index metamethod
func instanceIndex(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*InstanceWrapper)
	if !ok {
		l.ArgError(1, "instance expected")
		return 0
	}

	key := l.CheckString(2)

	// Build metadata for method access check
	accessMeta := make(registry.Metadata)
	accessMeta["binding_id"] = wrapper.instance.ID().String()
	accessMeta["method_name"] = key

	// Add instance scope to metadata
	if scope := wrapper.instance.Scope(); scope != nil {
		for k, v := range scope {
			accessMeta["scope."+k] = v
		}
	}

	// Add contracts this instance implements
	implementedContracts := wrapper.instance.Implements()
	contractIDs := make([]string, len(implementedContracts))
	for i, contractDef := range implementedContracts {
		contractIDs[i] = contractDef.ID().String()
	}
	accessMeta["contracts"] = contractIDs

	// Check if it's a method call by looking at the contracts
	for _, contractDef := range implementedContracts {
		if _, err := contractDef.Method(key); err == nil {
			// Method exists in contract, check security for method access
			if !security.IsAllowed(l.Context(), "contract.method.access", key, accessMeta) {
				l.Push(lua.LNil)
				return 1
			}

			// It's a valid method, return a function that calls it
			l.Push(l.NewClosure(func(l *lua.LState) int {
				// Reconstruct the call with the instance as first argument
				l.Insert(ud, 1)               // Insert instance userdata at position 1
				l.Insert(lua.LString(key), 2) // Insert method name at position 2
				return instanceCall(l)
			}))
			return 1
		}
	}

	// Not a method, return nil
	l.Push(lua.LNil)
	return 1
}
