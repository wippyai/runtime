// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestModule_Info(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "contract", info.Name)
	assert.Contains(t, info.Class, "workflow")
	assert.Contains(t, info.Class, "nondeterministic")
}

func TestModule_Build(t *testing.T) {
	tbl, yields := Module.Build()
	require.NotNil(t, tbl)
	require.NotEmpty(t, yields)

	assert.Equal(t, 4, len(yields))
	assert.Equal(t, contract.Open, yields[0].CmdID)
	assert.Equal(t, contract.Call, yields[1].CmdID)
	assert.Equal(t, contract.AsyncCall, yields[2].CmdID)
	assert.Equal(t, contract.AsyncCancel, yields[3].CmdID)
}

func TestOpenYield_Pool(t *testing.T) {
	y := AcquireOpenYield()
	require.NotNil(t, y)

	y.BindingID = registry.ParseID("test/binding")
	y.Scope = attrs.NewBagFrom(map[string]any{"key": "value"})

	ReleaseOpenYield(y)

	y2 := AcquireOpenYield()
	assert.Equal(t, registry.ID{}, y2.BindingID)
	assert.Nil(t, y2.Scope)
	ReleaseOpenYield(y2)
}

func TestOpenYield_ToCommand(t *testing.T) {
	y := AcquireOpenYield()
	defer ReleaseOpenYield(y)

	y.BindingID = registry.NewID("ns", "binding")
	y.Scope = attrs.NewBagFrom(map[string]any{"foo": "bar"})

	cmd := y.ToCommand()
	openCmd, ok := cmd.(*contract.OpenCmd)
	require.True(t, ok)
	assert.Equal(t, "ns", openCmd.BindingID.NS)
	assert.Equal(t, "binding", openCmd.BindingID.Name)
	assert.Equal(t, "bar", openCmd.Scope["foo"])
}

func TestOpenYield_CmdID(t *testing.T) {
	y := AcquireOpenYield()
	defer ReleaseOpenYield(y)
	assert.Equal(t, contract.Open, y.CmdID())
}

func TestCallYield_Pool(t *testing.T) {
	y := AcquireCallYield()
	require.NotNil(t, y)

	y.Method = "test_method"
	y.Args = payload.Payloads{payload.NewPayload("arg1", payload.JSON)}

	ReleaseCallYield(y)

	y2 := AcquireCallYield()
	assert.Empty(t, y2.Method)
	assert.Nil(t, y2.Args)
	ReleaseCallYield(y2)
}

func TestCallYield_ToCommand(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	y.Method = "get_data"
	y.Args = payload.Payloads{payload.NewPayload(42, payload.JSON)}

	cmd := y.ToCommand()
	callCmd, ok := cmd.(*contract.CallCmd)
	require.True(t, ok)
	assert.Equal(t, "get_data", callCmd.Method)
	assert.Len(t, callCmd.Args, 1)
}

func TestCallYield_CmdID(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)
	assert.Equal(t, contract.Call, y.CmdID())
}

func TestAsyncCallYield_Pool(t *testing.T) {
	y := AcquireAsyncCallYield()
	require.NotNil(t, y)

	y.Method = "async_method"
	y.Args = payload.Payloads{payload.NewPayload("data", payload.JSON)}
	y.Topic = "@future:test"

	ReleaseAsyncCallYield(y)

	y2 := AcquireAsyncCallYield()
	assert.Empty(t, y2.Method)
	assert.Nil(t, y2.Args)
	assert.Empty(t, y2.Topic)
	assert.Nil(t, y2.Future)
	ReleaseAsyncCallYield(y2)
}

func TestAsyncCallYield_ToCommand(t *testing.T) {
	y := AcquireAsyncCallYield()
	defer ReleaseAsyncCallYield(y)

	y.Method = "fetch"
	y.Topic = "@future:abc123"

	cmd := y.ToCommand()
	asyncCmd, ok := cmd.(*contract.AsyncCallCmd)
	require.True(t, ok)
	assert.Equal(t, "fetch", asyncCmd.Method)
	assert.Equal(t, "@future:abc123", asyncCmd.Topic)
}

func TestAsyncCallYield_CmdID(t *testing.T) {
	y := AcquireAsyncCallYield()
	defer ReleaseAsyncCallYield(y)
	assert.Equal(t, contract.AsyncCall, y.CmdID())
}

func TestAsyncCancelYield_Pool(t *testing.T) {
	y := AcquireAsyncCancelYield()
	require.NotNil(t, y)

	y.Topic = "@future:cancel-test"

	ReleaseAsyncCancelYield(y)

	y2 := AcquireAsyncCancelYield()
	assert.Empty(t, y2.Topic)
	ReleaseAsyncCancelYield(y2)
}

func TestAsyncCancelYield_CmdID(t *testing.T) {
	y := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y)
	assert.Equal(t, contract.AsyncCancel, y.CmdID())
}

func TestYield_StringAndType(t *testing.T) {
	tests := []struct {
		name       string
		yield      lua.LValue
		wantString string
	}{
		{"OpenYield", AcquireOpenYield(), "<contract_open_yield>"},
		{"CallYield", AcquireCallYield(), "<contract_call_yield>"},
		{"AsyncCallYield", AcquireAsyncCallYield(), "<contract_async_call_yield>"},
		{"AsyncCancelYield", AcquireAsyncCancelYield(), "<contract_async_cancel_yield>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantString, tt.yield.String())
			assert.Equal(t, lua.LTUserData, tt.yield.Type())
		})
	}
}

func TestOpenYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireOpenYield()
	defer ReleaseOpenYield(y)

	// Test with error
	results := y.HandleResult(l, nil, assert.AnError)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with nil data
	results = y.HandleResult(l, nil, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with wrong type
	results = y.HandleResult(l, "wrong type", nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])
}

func TestCallYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	// Test with error
	results := y.HandleResult(l, nil, assert.AnError)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with successful result
	results = y.HandleResult(l, contract.CallResult{Value: "hello"}, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LTString, results[0].Type())
	assert.Equal(t, lua.LNil, results[1])

	// Test with result error
	results = y.HandleResult(l, contract.CallResult{Error: assert.AnError}, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test various data types
	testCases := []struct {
		value    any
		name     string
		expected lua.LValueType
	}{
		{true, "bool", lua.LTBool},
		{42, "int", lua.LTInteger},
		{3.14, "float64", lua.LTNumber},
		{"test", "string", lua.LTString},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			y := AcquireCallYield()
			defer ReleaseCallYield(y)
			results := y.HandleResult(l, contract.CallResult{Value: tc.value}, nil)
			require.Len(t, results, 2)
			assert.Equal(t, tc.expected, results[0].Type())
			assert.Equal(t, lua.LNil, results[1])
		})
	}
}

func TestAsyncCancelYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y)

	// Test with error
	results := y.HandleResult(l, nil, assert.AnError)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test success
	results = y.HandleResult(l, nil, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LTrue, results[0])
	assert.Equal(t, lua.LNil, results[1])
}

func TestPoolConcurrency(_ *testing.T) {
	const goroutines = 50
	const iterations = 100

	done := make(chan struct{})

	// Test all yield pools concurrently
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				y1 := AcquireOpenYield()
				y1.BindingID = registry.ParseID("test/binding")
				ReleaseOpenYield(y1)

				y2 := AcquireCallYield()
				y2.Method = "test"
				ReleaseCallYield(y2)

				y3 := AcquireAsyncCallYield()
				y3.Topic = "@future:test"
				ReleaseAsyncCallYield(y3)

				y4 := AcquireAsyncCancelYield()
				y4.Topic = "@future:cancel"
				ReleaseAsyncCancelYield(y4)
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}
