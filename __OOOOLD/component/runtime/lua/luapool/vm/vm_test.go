package vm

import (
	"context"
	"testing"

	base64M "github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var result string

func BenchmarkNewVM(b *testing.B) {
	l, _ := zap.NewDevelopment()
	script := `
	function hello(args)
    		return args.hello
	end

	return hello
	`

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	vm, err := New(l, script, "hello", base64M.New())
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		res, err := vm.Execute(context.Background(), data)
		require.NoError(b, err)
		// to avoid compiler optimization
		result = res
	}
}
