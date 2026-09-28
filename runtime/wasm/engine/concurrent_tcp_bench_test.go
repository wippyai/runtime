// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// Both variants use eight concurrent loopback connections and the same clients.
// The WASM variant includes guest execution and the real socket dispatcher;
// neither variant includes outer process-scheduler routing. Reported ns/op is
// amortized throughput cost; mean-rtt-ns is the measured client round-trip time.
func BenchmarkConcurrentTCPEcho(b *testing.B) {
	for _, kind := range []string{"wasm", "go-reference"} {
		b.Run(kind, func(b *testing.B) {
			var ctx context.Context
			var address string
			var result <-chan concurrentTCPResult
			if kind == "wasm" {
				ctx, address, result = startConcurrentTCPGuest(b)
			} else {
				ctx, address, result = startNativeTCPReference(b)
			}
			type clientResult struct {
				err error
				rtt time.Duration
			}
			clients := make(chan clientResult, concurrentTCPClients)
			b.SetBytes(concurrentTCPFrame * 2)
			b.ReportAllocs()
			b.ResetTimer()
			for client := 0; client < concurrentTCPClients; client++ {
				frames := b.N / concurrentTCPClients
				if client < b.N%concurrentTCPClients {
					frames++
				}
				go func(client, frames int) {
					rtt, err := exchangeConcurrentTCP(ctx, address, client, frames)
					clients <- clientResult{rtt: rtt, err: err}
				}(client, frames)
			}
			var totalRTT time.Duration
			for range concurrentTCPClients {
				client := <-clients
				if client.err != nil {
					b.Fatal(client.err)
				}
				totalRTT += client.rtt
			}
			b.StopTimer()
			b.ReportMetric(float64(totalRTT.Nanoseconds())/float64(b.N), "mean-rtt-ns")
			select {
			case outcome := <-result:
				if outcome.err != nil || outcome.value != fmt.Sprintf("frames:%d", b.N) {
					b.Fatalf("server result: %+v", outcome)
				}
				if kind == "wasm" {
					b.ReportMetric(float64(outcome.waits)/float64(b.N), "dispatches/op")
				}
			case <-ctx.Done():
				b.Fatal(ctx.Err())
			}
		})
	}
}

func startNativeTCPReference(t testing.TB) (context.Context, string, <-chan concurrentTCPResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	result := make(chan concurrentTCPResult, 1)
	stopped := make(chan struct{})
	var mu sync.Mutex
	var conns []net.Conn
	closeAll := func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			conn.Close()
		}
	}
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		defer close(cancelDone)
		closeAll()
	})
	go func() {
		var wg sync.WaitGroup
		defer func() {
			closeAll()
			wg.Wait()
			if !stopCancel() {
				<-cancelDone
			}
			close(stopped)
		}()
		type finishedClient struct {
			err    error
			frames int
		}
		completed := make(chan finishedClient, concurrentTCPClients)
		for range concurrentTCPClients {
			conn, err := listener.Accept()
			if err != nil {
				result <- concurrentTCPResult{err: err}
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()
				frames, err := nativeEchoFrames(conn)
				completed <- finishedClient{frames: frames, err: err}
			}(conn)
		}
		total := 0
		for range concurrentTCPClients {
			client := <-completed
			if client.err != nil {
				result <- concurrentTCPResult{err: client.err}
				return
			}
			total += client.frames
		}
		result <- concurrentTCPResult{value: fmt.Sprintf("frames:%d", total)}
	}()
	t.Cleanup(func() {
		cancel()
		closeAll()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("native echo server did not stop")
		}
	})
	return ctx, listener.Addr().String(), result
}

func nativeEchoFrames(conn net.Conn) (int, error) {
	var frame [concurrentTCPFrame]byte
	frames := 0
	for {
		_, err := io.ReadFull(conn, frame[:])
		if err == io.EOF {
			return frames, nil
		}
		if err != nil {
			return frames, err
		}
		if _, err := conn.Write(frame[:]); err != nil {
			return frames, err
		}
		frames++
	}
}
