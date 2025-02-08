package list

import (
	"bytes"
	"context"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupDelegateTest(t *testing.T) *engine.VM {
	logger := zap.NewNop()

	// Setup loader with all required dependencies
	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterList(L, mod)
		protocol.RegisterKeyBinding(L, mod)
		render.RegisterStyle(L, mod)
		L.Push(mod)
		return 1
	}

	vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
	require.NoError(t, err)
	return vm
}

func TestLuaDelegate(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	// Register all required modules at once
	mod := vm.State().NewTable()
	RegisterList(vm.State(), mod)
	protocol.RegisterKeyBinding(vm.State(), mod)
	render.RegisterStyle(vm.State(), mod)
	vm.State().SetGlobal("btea", mod)

	// Basic methods
	t.Run("delegate basic methods", func(t *testing.T) {
		err := vm.DoString(nil, `
            -- Create basic list delegate
            local delegate = {
                height = 3,
                spacing = 1,
                render = function(model, index, item)
                    return string.format("Item %d: %s", index, item.title)
                end
            }

            -- Test direct property access
            assert(delegate.height == 3, "height should be 3")
            assert(delegate.spacing == 1, "spacing should be 1")

            -- Test render function
            local result = delegate.render({}, 0, {title = "Test"})
            assert(result == "Item 0: Test", "render should format correctly")
        `, "test_delegate_basic")
		require.NoError(t, err)
	})

	// Update handling
	t.Run("delegate update handling", func(t *testing.T) {
		err := vm.DoString(nil, `
            local updated = false
            
            local delegate = {
                update = function(self, msg, model)
                    if msg.type == "key" then
                        updated = true
                    end
                    return nil
                end
            }

            delegate:update({type = "key"}, {})
            assert(updated, "update should be called")
        `, "test_delegate_update")
		require.NoError(t, err)
	})

	// Help system
	t.Run("delegate help system", func(t *testing.T) {
		err := vm.DoString(nil, `
            local delegate = {
                short_help = {
                    btea.bind({
                        keys = {"enter"},
                        help = {key = "enter", desc = "select"}
                    })
                }
            }

            assert(type(delegate.short_help[1]) == "userdata", "should have binding")
        `, "test_delegate_help")
		require.NoError(t, err)
	})

	// Test styling
	t.Run("delegate styling", func(t *testing.T) {
		err := vm.DoString(nil, `
            local delegate = {
                render = function(model, index, item)
                    local style = btea.style()
                    if index == 0 then
                        style = style:foreground("#00ff00")
                    end
                    return style:render(item.title)
                end
            }

            local result = delegate.render({}, 0, {title = "Test"})
            assert(type(result) == "string", "styled render should return string")
        `, "test_delegate_styling")
		require.NoError(t, err)
	})
}

// TestDelegateRender tests standalone render
func TestDelegateRender(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	// Register modules
	mod := vm.State().NewTable()
	RegisterList(vm.State(), mod)
	vm.State().SetGlobal("btea", mod)

	delegate := &LuaDelegate{
		luaDelegate: vm.State().NewTable(),
		luaState:    vm.State(),
	}

	// Set render function
	vm.State().SetField(delegate.luaDelegate, "render", vm.State().NewFunction(
		func(L *lua.LState) int {
			L.Push(lua.LString("test"))
			return 1
		},
	))

	buf := &bytes.Buffer{}
	delegate.Render(buf, list.Model{}, 0, &LuaItem{
		value:    vm.State().NewTable(),
		luaState: vm.State(),
	})

	require.Equal(t, "test", buf.String())
}

// Test updating through tea.Model interface
func TestDelegateUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register required modules
	mod := cvm.State().NewTable()
	RegisterList(cvm.State(), mod)
	protocol.RegisterKeyBinding(cvm.State(), mod)
	render.RegisterStyle(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	err = cvm.StartString(context.Background(), `
        local list = btea.new_list({
            width = 40,
            height = 20,
            delegate = {
                height = 1,
                spacing = 1,
                update = function(msg, model)
                    if msg.type == "key" and msg.key == "x" then
                        return function()
                            return { type = "custom", value = "handled" }
                        end
                    end
                end,
                render = function(w, m, index, item)
                    return item.title
                end
            },
            items = {{ title = "Test" }}
        })

        -- Initial state
        local msg = coroutine.yield("ready", nil)
        
        -- Handle custom key
        local cmd = list:update(msg)
        coroutine.yield("handled", cmd)
        
        coroutine.yield("done", nil)
    `, "test_delegate_tea_update")
	require.NoError(t, err)

	// First yield - ready for input
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, "ready", tasks[0].Yielded[0].String())

	// Send key message
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "handled", tasks[0].Yielded[0].String())

	// Final step
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}
