// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	actorhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
)

// TestQuickJSActor_StateAndIsolation drives the real scheduler and relay. The
// guest keeps QuickJS alive and suspends its JavaScript receive loop between
// messages; the second PID must own independent JavaScript state.
func TestQuickJSActor_StateAndIsolation(t *testing.T) {
	fixture := os.Getenv("WIPPY_QUICKJS_ACTOR_FIXTURE")
	if fixture == "" {
		fixture = "testdata/quickjs_actor.wasm"
	}
	code, err := os.ReadFile(fixture)
	require.NoError(t, err)
	factory := func() (processapi.Process, error) {
		return createWASMActorProcessWithExecutionLimits(context.Background(), code, 64<<20, actorhost.DefaultLimits(), wasmapi.LimitsConfig{})
	}
	cluster := newHarnessCluster(t, 2, factory)
	first := cluster.SpawnActor(t, "quickjs-first")
	second := cluster.SpawnActor(t, "quickjs-second")
	firstDone := cluster.lifecycle.registerWait(first)
	secondDone := cluster.lifecycle.registerWait(second)
	client := cluster.NewClient("quickjs-client")
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request := func(target pid.PID, topic, expected string) {
		t.Helper()
		reply, _, err := client.Request(ctx, target, topic)
		if err != nil {
			cluster.lifecycle.mu.Lock()
			for name, result := range cluster.lifecycle.completed {
				t.Logf("completed %s: %+v", name, result)
			}
			cluster.lifecycle.mu.Unlock()
		}
		require.NoError(t, err)
		require.Equal(t, "quickjs", reply.Topic)
		require.Len(t, reply.Payloads, 1)
		data, ok := reply.Payloads[0].Data().([]byte)
		require.True(t, ok)
		require.Equal(t, expected, string(data))
	}
	for i := 1; i <= 16; i++ {
		request(first, "increment", fmt.Sprintf("count:%d", i))
	}
	request(first, "unrecognized", "unknown:unrecognized")
	request(first, "increment", "count:17")
	request(second, "increment", "count:1")
	request(first, "increment", "count:18")
	require.NoError(t, client.SendOnly(first, "stop"))
	select {
	case result := <-firstDone:
		require.NotNil(t, result)
		require.NoError(t, result.Error)
	case <-ctx.Done():
		t.Fatal("QuickJS actor did not exit after stop")
	}
	// Terminate the other actor while its JavaScript receive call is parked.
	require.NoError(t, cluster.host.Terminate(ctx, second))
	select {
	case result := <-secondDone:
		require.NotNil(t, result)
	case <-ctx.Done():
		t.Fatal("parked QuickJS actor did not terminate")
	}
	require.Equal(t, uint32(1), cluster.lifecycle.completionCount(first))
	require.Equal(t, uint32(1), cluster.lifecycle.completionCount(second))
}

// BenchmarkQuickJSActorRoundTrip includes JavaScript, Asyncify, scheduler and
// relay routing. Component compilation and the first request are excluded.
func BenchmarkQuickJSActorRoundTrip(b *testing.B) {
	fixture := os.Getenv("WIPPY_QUICKJS_ACTOR_FIXTURE")
	if fixture == "" {
		fixture = "testdata/quickjs_actor.wasm"
	}
	code, err := os.ReadFile(fixture)
	require.NoError(b, err)
	factory := func() (processapi.Process, error) {
		return createWASMActorProcessWithExecutionLimits(context.Background(), code, 64<<20, actorhost.DefaultLimits(), wasmapi.LimitsConfig{})
	}
	startup := time.Now()
	cluster := newHarnessCluster(b, 1, factory)
	target := cluster.SpawnActor(b, "quickjs-benchmark")
	client := cluster.NewClient("quickjs-benchmark-client")
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, _, err = client.Request(ctx, target, "increment")
	require.NoError(b, err)
	cold := time.Since(startup)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reply, _, err := client.Request(ctx, target, "increment")
		if err != nil {
			b.Fatal(err)
		}
		if reply.Topic != "quickjs" || len(reply.Payloads) != 1 {
			b.Fatal("invalid QuickJS reply")
		}
		data, ok := reply.Payloads[0].Data().([]byte)
		if !ok || string(data) != fmt.Sprintf("count:%d", i+2) {
			b.Fatal("incorrect JavaScript counter state")
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(cold.Nanoseconds())/1e6, "cold-start-ms")
}
