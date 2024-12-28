package pool

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"sync"
	"testing"
	"time"

	base64M "github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func TestNewVM(t *testing.T) {
	l := zap.NewNop()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	vm, err := engine.NewVM(l, script, "hello")
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
	l := zap.NewNop()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	vm, err := engine.NewVM(l, script, "hello", base64M.NewBase64Module())
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
	scripts := make(map[string]*Config)
	scripts[scriptId] = NewPoolCfg(2, script, "hello")
	p, err := NewLuaPool(l, scripts, WithModules(base64M.NewBase64Module()), WithPollTimeout(time.Second*2))
	require.NoError(t, err)

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	resp := p.Queue(context.Background(), NewPoolTask(scriptId, data))

	res := <-resp
	// to avoid compiler optimization
	result = res
}

func TestPoolConcurrent(t *testing.T) {
	l := zap.NewNop()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	scriptID := "foo"
	scripts := make(map[string]*Config)

	scripts[scriptID] = NewPoolCfg(10, script, "hello")
	p, err := NewLuaPool(l, scripts, WithModules(base64M.NewBase64Module()), WithPollTimeout(time.Second*10))
	require.NoError(t, err)

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	wg := &sync.WaitGroup{}
	wg.Add(100000)

	tt := time.Now()
	for i := 0; i < 100000; i++ {
		go func() {
			resp := p.Queue(context.Background(), NewPoolTask(scriptID, data))
			for range resp {
			}

			wg.Done()
		}()
	}

	wg.Wait()
	t.Log(time.Since(tt))
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
	scripts := make(map[string]*Config)
	scripts[scriptId] = NewPoolCfg(10, script, "hello")
	p, err := NewLuaPool(l, scripts, WithModules(base64M.NewBase64Module()), WithPollTimeout(time.Second*2))
	require.NoError(b, err)

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := p.Queue(context.Background(), NewPoolTask(scriptId, data))
		for res := range resp {
			// to avoid compiler optimization
			result = res
		}
	}

}
