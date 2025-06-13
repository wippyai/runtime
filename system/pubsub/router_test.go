package pubsub

import (
	"errors"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/pubsub"
)

// mockReceiver is a mock implementation of the pubsub.Receiver interface for testing.
// It allows us to track calls to Send, inspect the received package, and simulate errors.
type mockReceiver struct {
	mu         sync.Mutex
	sendCalled int
	lastPkg    *pubsub.Package
	returnErr  error
}

// Send implements the pubsub.Receiver interface.
func (m *mockReceiver) Send(pkg *pubsub.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendCalled++
	m.lastPkg = pkg
	return m.returnErr
}

// reset clears the mock's state for the next test case.
func (m *mockReceiver) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendCalled = 0
	m.lastPkg = nil
	m.returnErr = nil
}

// wasCalled returns true if the Send method was invoked.
func (m *mockReceiver) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendCalled > 0
}

// newMockReceiver creates a new mock receiver.
func newMockReceiver() *mockReceiver {
	return &mockReceiver{}
}

func TestNewRouter(t *testing.T) {
	t.Run("creates router with upstreams and internode", func(t *testing.T) {
		mockUpstream := newMockReceiver()
		mockInternode := newMockReceiver()
		upstreams := map[pubsub.NodeID]pubsub.Receiver{
			"node-a": mockUpstream,
		}

		r := NewRouter(upstreams, mockInternode)

		if r == nil {
			t.Fatal("NewRouter returned nil")
		}
		if r.internode != mockInternode {
			t.Error("internode receiver was not set correctly")
		}

		val, ok := r.upstreams.Load("node-a")
		if !ok || val != mockUpstream {
			t.Error("upstream 'node-a' was not stored correctly")
		}
	})

	t.Run("upstream map is copied and immutable", func(t *testing.T) {
		originalMock := newMockReceiver()
		upstreams := map[pubsub.NodeID]pubsub.Receiver{
			"node-a": originalMock,
		}

		r := NewRouter(upstreams, nil)

		// Mutate the original map after creating the router
		upstreams["node-a"] = newMockReceiver() // Replace entry
		upstreams["node-b"] = newMockReceiver() // Add new entry
		delete(upstreams, "node-a")             // Delete original entry

		// The router's internal state should be unaffected
		val, ok := r.upstreams.Load("node-a")
		if !ok || val != originalMock {
			t.Fatal("Router's upstream was affected by mutation of the original map")
		}

		_, ok = r.upstreams.Load("node-b")
		if ok {
			t.Fatal("Router should not contain 'node-b' which was added to the original map after creation")
		}
	})
}

func TestRouter_Send(t *testing.T) {
	// --- Setup Mocks and Routers ---
	upstreamA := newMockReceiver()
	upstreamB := newMockReceiver()
	internode := newMockReceiver()

	// Router with multiple upstreams and a fallback
	routerWithFallback := NewRouter(map[pubsub.NodeID]pubsub.Receiver{
		"node-a": upstreamA,
		"node-b": upstreamB,
	}, internode)

	// Router with only one upstream and no fallback
	routerWithoutFallback := NewRouter(map[pubsub.NodeID]pubsub.Receiver{
		"node-a": upstreamA,
	}, nil)

	// --- Define Test Cases ---
	testCases := []struct {
		name          string
		router        *Router
		pkg           *pubsub.Package
		setup         func() // Optional setup function for mocks
		wantErr       bool
		expectedErr   string
		checkReceives func(t *testing.T)
	}{
		{
			name:   "Route to specific upstream 'node-a'",
			router: routerWithFallback,
			pkg:    &pubsub.Package{Target: pubsub.PID{Node: "node-a"}},
			checkReceives: func(t *testing.T) {
				if upstreamA.sendCalled != 1 {
					t.Errorf("expected upstreamA to be called once, got %d", upstreamA.sendCalled)
				}
				if upstreamB.wasCalled() {
					t.Error("upstreamB should not have been called")
				}
				if internode.wasCalled() {
					t.Error("internode should not have been called")
				}
				if upstreamA.lastPkg.Target.Node != "node-a" {
					t.Errorf("upstreamA received wrong package")
				}
			},
		},
		{
			name:   "Route to specific upstream 'node-b'",
			router: routerWithFallback,
			pkg:    &pubsub.Package{Target: pubsub.PID{Node: "node-b"}},
			checkReceives: func(t *testing.T) {
				if upstreamB.sendCalled != 1 {
					t.Errorf("expected upstreamB to be called once, got %d", upstreamB.sendCalled)
				}
				if upstreamA.wasCalled() {
					t.Error("upstreamA should not have been called")
				}
				if internode.wasCalled() {
					t.Error("internode should not have been called")
				}
			},
		},
		{
			name:   "Fallback to internode when upstream is not found",
			router: routerWithFallback,
			pkg:    &pubsub.Package{Target: pubsub.PID{Node: "unknown-node"}},
			checkReceives: func(t *testing.T) {
				if internode.sendCalled != 1 {
					t.Errorf("expected internode to be called once, got %d", internode.sendCalled)
				}
				if upstreamA.wasCalled() || upstreamB.wasCalled() {
					t.Error("specific upstreams should not have been called")
				}
			},
		},
		{
			name:        "Error when no matching upstream and no fallback",
			router:      routerWithoutFallback,
			pkg:         &pubsub.Package{Target: pubsub.PID{Node: "unknown-node"}},
			wantErr:     true,
			expectedErr: "router: no upstream for node unknown-node",
			checkReceives: func(t *testing.T) {
				if upstreamA.wasCalled() {
					t.Error("upstreamA should not have been called")
				}
			},
		},
		{
			name:        "Error on nil package",
			router:      routerWithFallback,
			pkg:         nil,
			wantErr:     true,
			expectedErr: "nil package",
			checkReceives: func(t *testing.T) {
				if upstreamA.wasCalled() || upstreamB.wasCalled() || internode.wasCalled() {
					t.Error("no receiver should be called for a nil package")
				}
			},
		},
		{
			name:   "Propagate error from upstream",
			router: routerWithFallback,
			pkg:    &pubsub.Package{Target: pubsub.PID{Node: "node-a"}},
			setup: func() {
				upstreamA.returnErr = errors.New("network connection failed")
			},
			wantErr:     true,
			expectedErr: "network connection failed",
			checkReceives: func(t *testing.T) {
				if upstreamA.sendCalled != 1 {
					t.Error("upstreamA should have been called despite returning an error")
				}
			},
		},
		{
			name:   "Propagate error from internode",
			router: routerWithFallback,
			pkg:    &pubsub.Package{Target: pubsub.PID{Node: "unknown-node"}},
			setup: func() {
				internode.returnErr = errors.New("internode service unavailable")
			},
			wantErr:     true,
			expectedErr: "internode service unavailable",
			checkReceives: func(t *testing.T) {
				if internode.sendCalled != 1 {
					t.Error("internode should have been called despite returning an error")
				}
			},
		},
	}

	// --- Run Test Cases ---
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset all mocks before each test
			upstreamA.reset()
			upstreamB.reset()
			internode.reset()

			// Run optional setup for the specific test case
			if tc.setup != nil {
				tc.setup()
			}

			// Execute the Send method
			err := tc.router.Send(tc.pkg)

			// Assert error
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error but got nil")
				}
				if err.Error() != tc.expectedErr {
					t.Errorf("expected error message '%s', got '%s'", tc.expectedErr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("did not expect an error but got: %v", err)
				}
			}

			// Check which mocks were called
			if tc.checkReceives != nil {
				tc.checkReceives(t)
			}
		})
	}
}
