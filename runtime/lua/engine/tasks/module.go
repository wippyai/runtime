package tasks

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"log"
	"sync"
)

const Channel = "layer.tasks"

type task struct {
	id       string
	value    []lua.LValue
	respChan chan<- response
	closed   bool
	mu       sync.Mutex
}

type response struct {
	value  lua.LValue
	closed bool // true to close channel after sending
}

// Module represents task Lua module
type Module struct{}

func NewTaskModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "tasks"
}

// Loader registers module functions and types
func (m *Module) Loader(L *lua.LState) int {
	// Create module table
	mod := L.NewTable()

	// Register receive function
	L.SetField(mod, "receive", L.NewFunction(receiveLua))

	// task methods
	taskMethods := map[string]lua.LGFunction{
		"write": writeLua,
		"done":  doneLua,
	}

	// task metatable
	mt := L.NewTypeMetatable("task")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), taskMethods))

	// Register module
	L.Push(mod)
	return 1
}

// receiveLua implements tasks.receive() -> creates a named channel using Channel constant
func receiveLua(L *lua.LState) int {
	log.Printf("tasks.receive() -> creates a named channel using Channel constant")

	// Create a named channel with default buffer size of 0, todo: we can bump buffer
	ch := channel.Named(Channel, 0)
	ud := L.NewUserData()
	ud.Value = ch
	ch.LinkValue(ud)
	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)
	return 1
}

// writeLua implements task:write(value) -> sends value to the task channel
func writeLua(L *lua.LState) int {
	t := checkTask(L)
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		L.RaiseError("t already closed")
		return 0
	}

	value := L.CheckAny(2)
	t.respChan <- response{value: value, closed: false}
	return 0
}

// doneLua implements task:done() -> closes the task channel
func doneLua(L *lua.LState) int {
	t := checkTask(L)
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		L.RaiseError("t already closed")
		return 0
	}

	t.closed = true
	t.respChan <- response{closed: true}
	close(t.respChan)
	return 0
}

// Helper function to validate task object
func checkTask(L *lua.LState) *task {
	ud := L.CheckUserData(1)
	if ch, ok := ud.Value.(*task); ok {
		return ch
	}
	L.ArgError(1, "task expected")
	return nil
}
