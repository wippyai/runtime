package protocol

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

// Simple direct implementation of tea.Model for testing
type testModel struct {
	InitCmd  tea.Cmd
	UpdateFn func(msg tea.Msg) (tea.Model, tea.Cmd)
	ViewStr  string
}

func (m testModel) Init() tea.Cmd                           { return m.InitCmd }
func (m testModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m.UpdateFn(msg) }
func (m testModel) View() string                            { return m.ViewStr }

func TestLuaModelWrapper(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("basic wrapper functionality", func(t *testing.T) {
		// Spawn a simple Lua model table
		model := l.NewTable()
		l.SetField(model, "init", l.NewFunction(func(l *lua.LState) int {
			l.Push(lua.LNil)
			return 1
		}))
		l.SetField(model, "update", l.NewFunction(func(l *lua.LState) int {
			l.Push(l.Get(1)) // return same model
			l.Push(lua.LNil) // no command
			return 2
		}))
		l.SetField(model, "view", l.NewFunction(func(l *lua.LState) int {
			l.Push(lua.LString("test"))
			return 1
		}))

		wrapper := &LuaModelWrapper{value: model, luaState: l}

		// Test all methods
		assert.Nil(t, wrapper.Init(), "Init should return nil")
		model2, cmd := wrapper.Update(nil)
		assert.Equal(t, wrapper, model2)
		assert.Nil(t, cmd)
		assert.Equal(t, "test", wrapper.View())
	})

	t.Run("model state updates", func(t *testing.T) {
		model := l.NewTable()
		l.SetField(model, "counter", lua.LNumber(0))
		l.SetField(model, "update", l.NewFunction(func(l *lua.LState) int {
			self := l.CheckTable(1)
			counter := self.RawGetString("counter").(lua.LNumber)
			self.RawSetString("counter", lua.LNumber(counter+1))
			l.Push(self)
			l.Push(lua.LNil)
			return 2
		}))
		l.SetField(model, "init", l.NewFunction(func(l *lua.LState) int {
			l.Push(lua.LNil)
			return 1
		}))
		l.SetField(model, "view", l.NewFunction(func(l *lua.LState) int {
			l.Push(lua.LString(""))
			return 1
		}))

		wrapper := &LuaModelWrapper{value: model, luaState: l}
		wrapper.Update(nil)
		counter := wrapper.value.(*lua.LTable).RawGetString("counter")
		assert.Equal(t, lua.LNumber(1), counter)
	})
}

func TestTryGetModel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("model types", func(t *testing.T) {
		// Test direct model
		directModel := testModel{
			ViewStr:  "test",
			UpdateFn: func(msg tea.Msg) (tea.Model, tea.Cmd) { return nil, nil },
		}
		model, ok := TryGetModel(l, l.NewUserData())
		assert.False(t, ok, "should reject empty userdata")

		// Test direct model in userdata
		ud := l.NewUserData()
		ud.Value = directModel
		model, ok = TryGetModel(l, ud)
		assert.True(t, ok, "should accept proper model")
		resultModel, ok := model.(testModel)
		assert.True(t, ok, "should be testModel type")
		assert.Equal(t, directModel.ViewStr, resultModel.ViewStr)
		// Don't compare UpdateFn directly as functions don't compare well
		assert.NotNil(t, resultModel.UpdateFn)

		// Test Lua table with model methods
		luaModel := l.NewTable()
		l.SetField(luaModel, "init", l.NewFunction(func(l *lua.LState) int { return 0 }))
		l.SetField(luaModel, "update", l.NewFunction(func(l *lua.LState) int { return 0 }))
		l.SetField(luaModel, "view", l.NewFunction(func(l *lua.LState) int { return 0 }))
		model, ok = TryGetModel(l, luaModel)
		assert.True(t, ok, "should accept Lua model")
		_, isWrapper := model.(*LuaModelWrapper)
		assert.True(t, isWrapper)
	})
}

func TestUpdateModelValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("model updates", func(t *testing.T) {
		// Spawn two models of same type
		model1 := testModel{ViewStr: "model1"}
		model2 := testModel{ViewStr: "model2"}

		// Test with invalid values
		assert.False(t, UpdateModelValue(l, lua.LNil, nil), "should reject nil")

		ud := l.NewUserData()
		ud.Value = "not a model"
		assert.False(t, UpdateModelValue(l, ud, nil), "should reject non-model userdata")

		// Test with valid models
		ud = l.NewUserData()
		ud.Value = model1
		assert.True(t, UpdateModelValue(l, ud, model2), "should accept same type")
		assert.Equal(t, model2, ud.Value)

		// Test with wrapper models
		wrapper1 := &LuaModelWrapper{value: l.NewTable(), luaState: l}
		wrapper2 := &LuaModelWrapper{value: l.NewTable(), luaState: l}
		ud.Value = wrapper1
		assert.True(t, UpdateModelValue(l, ud, wrapper2), "should accept wrapper update")

		// Test with different types
		assert.False(t, UpdateModelValue(l, ud, model1), "should reject different types")
	})
}
