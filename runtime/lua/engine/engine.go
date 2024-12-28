package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
)

// Engine encapsulates the Lua state and config
type Engine struct {
	log      *zap.Logger
	L        *lua.LState
	printBuf *strings.Builder
}

// NewLuaEngine creates and initializes a new LuaEngine
func NewLuaEngine(ctx context.Context, log *zap.Logger) *Engine {
	log.Debug("initializing engine")
	L := lua.NewState()
	L.SetContext(ctx)

	sb := &strings.Builder{}

	// Override print function
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {

		// todo: check for context
		///L.Context().Value("logger").(*zap.Logger).Info("print", zap.String("output", L.ToString(1)))

		top := L.GetTop()
		var parts []string
		for i := 1; i <= top; i++ {
			parts = append(parts, L.ToString(i))
		}
		output := strings.Join(parts, "\t")
		fmt.Fprintf(sb, "print:%s\n", output)

		return 0
	}))

	return &Engine{
		log:      log,
		printBuf: sb,
		L:        L,
	}
}

// PreloadLibrary preloads module based on the mtype - module type (cloud, local), and module name
func (e *Engine) PreloadLibrary(name, code string) {
	e.L.PreloadModule(name, func(L *lua.LState) int {
		fn, err := L.Load(strings.NewReader(code), fmt.Sprintf("<%s>", name))
		if err != nil {
			return 0
		}

		L.Push(fn)
		err = L.PCall(0, lua.MultRet, nil)
		if err != nil {
			return 0
		}
		return 1
	})
}

// Close closes the Lua state
func (e *Engine) Close() {
	e.printBuf.Reset()
	e.L.Close()
}

func (e *Engine) GetPrints() string {
	return e.printBuf.String()
}

// DoString executes a Lua string
func (e *Engine) DoString(s, name string) error {
	fn, err := e.L.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
	if err != nil {
		return err
	}

	e.L.Push(fn)

	return e.L.PCall(0, lua.MultRet, nil)
}

// SetGlobal sets a global variable in the Lua state
func (e *Engine) SetGlobal(name string, value lua.LValue) {
	e.L.SetGlobal(name, value)
}

// Get returns the value at the top of the stack and pops it
func (e *Engine) Get(index int) lua.LValue {
	return e.L.Get(index)
}

// Pop pops n elements from the stack
func (e *Engine) Pop(n int) {
	e.L.Pop(n)
}

func GoToLua(l *lua.LState, v any) lua.LValue {
	switch v := v.(type) {
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case nil:
		return lua.LNil
	case []int:
		table := l.NewTable()
		for i, v := range v {
			l.SetTable(table, lua.LNumber(i+1), lua.LNumber(v))
		}
		return table
	case []string:
		table := l.NewTable()
		for i, v := range v {
			l.SetTable(table, lua.LNumber(i+1), lua.LString(v))
		}
		return table
	case map[string]any:
		table := l.NewTable()
		for k, v := range v {
			l.SetTable(table, lua.LString(k), GoToLua(l, v))
		}
		return table
	case map[string]string:
		table := l.NewTable()
		for k, v := range v {
			l.SetTable(table, lua.LString(k), lua.LString(v))
		}
		return table
	case []any:
		table := l.NewTable()
		for i, v := range v {
			l.SetTable(table, lua.LNumber(i+1), GoToLua(l, v))
		}
		return table
	default:
		return lua.LNil
	}
}

func ToGoAny(v lua.LValue) any {
	switch v.Type() { //nolint:exhaustive
	case lua.LTNil:
		return v.String()
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		return float64(v.(lua.LNumber))
	case lua.LTString:
		return string(v.(lua.LString))
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		maxn := tbl.MaxN()
		if maxn == 0 { // Table is being used as a map
			result := make(map[string]any)
			tbl.ForEach(func(key, value lua.LValue) {
				result[key.String()] = ToGoAny(value)
			})
			return result
		} else { // Table is being used as an array
			result := make([]any, 0, maxn)
			for i := 1; i <= maxn; i++ {
				result = append(result, ToGoAny(tbl.RawGetInt(i)))
			}
			return result
		}
	default:
		return v.String()
	}
}
