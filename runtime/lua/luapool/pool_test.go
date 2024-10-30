package pool

import (
	"context"
	"testing"
	"time"

	base64M "github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/stretchr/testify/require"

	"github.com/ponyruntime/pony/runtime/lua/luapool/vm"
	"go.uber.org/zap"
)

func TestNewVM(t *testing.T) {
	l, _ := zap.NewDevelopment()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	vm, err := vm.New(l, script, "hello")
	require.NoError(t, err)
	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	res, err := vm.Execute(context.Background(), data)
	require.NoError(t, err)
	t.Log(res)

	res, err = vm.Execute(context.Background(), data)
	require.NoError(t, err)
	t.Log(res)

	res, err = vm.Execute(context.Background(), data)
	require.NoError(t, err)
	t.Log(res)
}

func TestNewVM2(t *testing.T) {
	l, _ := zap.NewDevelopment()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	vm, err := vm.New(l, script, "hello", base64M.New())
	require.NoError(t, err)
	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	res, err := vm.Execute(context.Background(), data)
	require.NoError(t, err)
	t.Log(res)

	res, err = vm.Execute(context.Background(), data)
	require.NoError(t, err)
	t.Log(res)

	res, err = vm.Execute(context.Background(), data)
	require.NoError(t, err)
	t.Log(res)
}

func TestPool(t *testing.T) {
	l, _ := zap.NewDevelopment()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	scriptId := "foo"
	scripts := make(map[string]*PoolCfg)
	scripts[scriptId] = &PoolCfg{
		NumVMs: 2,
		Script: script,
		Main:   "hello",
	}

	p, err := NewLuaPool(l, scripts, WithModules(base64M.New()), WithPollTimeout(time.Second*2), WithNumWorkers(10))
	require.NoError(t, err)

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	resp := make(chan string, 1)
	ts := &Task{
		ScriptID: scriptId,
		Args:     data,
		Resp:     resp,
	}
	p.Queue(ts)

	res := <-resp
	// to avoid compiler optimization
	result = res
}

var result string

func BenchmarkPool(b *testing.B) {
	l := zap.NewNop()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	scriptId := "foo"
	scripts := make(map[string]*PoolCfg)
	scripts[scriptId] = &PoolCfg{
		NumVMs: 10,
		Script: script,
		Main:   "hello",
	}

	p, err := NewLuaPool(l, scripts, WithModules(base64M.New()), WithPollTimeout(time.Second*2), WithNumWorkers(10))
	require.NoError(b, err)

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := make(chan string, 1)
		ts := &Task{
			ScriptID: scriptId,
			Args:     data,
			Resp:     resp,
		}
		p.Queue(ts)

		res := <-resp
		// to avoid compiler optimization
		result = res
	}

}
