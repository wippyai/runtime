// SPDX-License-Identifier: MPL-2.0

package static

import (
	"context"
	"sync"
	"testing"

	"github.com/wippyai/runtime/system/scheduler/affinity"
)

func TestStaticPinThreadCompletesCalls(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{}, Config{Workers: 3, PinThread: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	var wg sync.WaitGroup
	const n = 200
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			result, err := p.Call(context.Background(), "test", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
				return
			}
			if result.Error != nil {
				t.Errorf("result error: %v", result.Error)
			}
		}()
	}
	wg.Wait()
}

func TestStaticPinThreadWithEmptyAffinityIsNoop(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{}, Config{
		Workers:   2,
		PinThread: true,
		Affinity:  affinity.Set{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	result, err := p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}
