package graph

import (
	"container/heap"
	"reflect"
	"sync"
	"testing"
)

func TestShortestPathScenarios(t *testing.T) {
	t.Run("complex paths", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// Test graph structure:
		//    A ---4--> B ---3--> E
		//    |         ^         ^
		//  2 |         | 1       | 2
		//    v    5    |         |
		//    C ------> D --------'

		nodes := []string{"A", "B", "C", "D", "E"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edges := []struct {
			from, to string
			weight   int
			info     string
		}{
			{"A", "B", 4, "path1"},
			{"A", "C", 2, "path2"},
			{"C", "D", 5, "path3"},
			{"D", "B", 1, "path4"},
			{"B", "E", 3, "path5"},
			{"D", "E", 2, "path6"},
		}

		for _, e := range edges {
			g.AddEdge(e.from, e.to, e.weight, TestEdgeData{Label: e.info})
		}

		tests := []struct {
			name     string
			from, to string
			want     *Path[string]
			wantErr  bool
		}{
			{
				name: "shortest via multiple nodes",
				from: "A",
				to:   "E",
				want: &Path[string]{
					Nodes: []string{"A", "B", "E"},
					Cost:  7,
				},
			},
			{
				name: "alternative longer path",
				from: "A",
				to:   "B",
				want: &Path[string]{
					Nodes: []string{"A", "B"},
					Cost:  4,
				},
			},
			{
				name:    "impossible path",
				from:    "E",
				to:      "A",
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := g.ShortestPath(tt.from, tt.to)
				if (err != nil) != tt.wantErr {
					t.Errorf("ShortestPath() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("ShortestPath() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("edge cases", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		tests := []struct {
			name     string
			from, to string
			setup    func()
			wantErr  bool
		}{
			{
				name:    "single node path",
				from:    "A",
				to:      "A",
				setup:   func() {},
				wantErr: false,
			},
			{
				name: "disconnected nodes",
				from: "A",
				to:   "B",
				setup: func() {
					// No edges added - disconnected
				},
				wantErr: true,
			},
			{
				name: "self loop",
				from: "A",
				to:   "A",
				setup: func() {
					g.AddEdge("A", "A", 1, TestEdgeData{Label: "loop"})
				},
				wantErr: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.setup()
				_, err := g.ShortestPath(tt.from, tt.to)
				if (err != nil) != tt.wantErr {
					t.Errorf("ShortestPath() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})
}

func TestShortestPathConcurrent(t *testing.T) {
	g := New[string, TestEdgeData]()

	// Setup test graph
	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		g.AddNode(node)
	}

	edgeData := TestEdgeData{Label: "path"}
	g.AddEdge("A", "B", 1, edgeData)
	g.AddEdge("B", "C", 2, edgeData)
	g.AddEdge("C", "D", 3, edgeData)
	g.AddEdge("A", "D", 10, edgeData) // Longer direct path

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := g.ShortestPath("A", "D")
			if err != nil {
				t.Errorf("concurrent ShortestPath() error = %v", err)
			}
			if path != nil && path.Cost != 6 { // 1 + 2 + 3 = 6
				t.Errorf("concurrent ShortestPath() wrong cost = %v", path.Cost)
			}
		}()
	}
	wg.Wait()
}

func TestShortestPathGenericTypes(t *testing.T) {
	t.Run("integer nodes", func(t *testing.T) {
		g := New[int, TestEdgeData]()
		nodes := []int{1, 2, 3, 4}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "int_path"}
		g.AddEdge(1, 2, 1, edgeData)
		g.AddEdge(2, 3, 2, edgeData)
		g.AddEdge(3, 4, 3, edgeData)
		g.AddEdge(1, 4, 10, edgeData) // Longer direct path

		path, err := g.ShortestPath(1, 4)
		if err != nil {
			t.Fatalf("ShortestPath() error = %v", err)
		}

		want := &Path[int]{
			Nodes: []int{1, 2, 3, 4},
			Cost:  6,
		}
		if !reflect.DeepEqual(path, want) {
			t.Errorf("ShortestPath() = %v, want %v", path, want)
		}
	})

	t.Run("custom type nodes", func(t *testing.T) {
		type CustomID int
		g := New[CustomID, TestEdgeData]()

		nodes := []CustomID{1, 2, 3}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "custom_path"}
		g.AddEdge(CustomID(1), CustomID(2), 1, edgeData)
		g.AddEdge(CustomID(2), CustomID(3), 2, edgeData)

		path, err := g.ShortestPath(CustomID(1), CustomID(3))
		if err != nil {
			t.Fatalf("ShortestPath() error = %v", err)
		}

		want := &Path[CustomID]{
			Nodes: []CustomID{1, 2, 3},
			Cost:  3,
		}
		if !reflect.DeepEqual(path, want) {
			t.Errorf("ShortestPath() = %v, want %v", path, want)
		}
	})
}

func TestPriorityQueue(t *testing.T) {
	t.Run("basic operations", func(t *testing.T) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)

		// Test empty queue
		if pq.Len() != 0 {
			t.Errorf("New queue should be empty, got length %d", pq.Len())
		}

		// AddCleanup test items
		heap.Push(pq, &item[string]{node: "A", priority: 3})
		heap.Push(pq, &item[string]{node: "B", priority: 1})
		heap.Push(pq, &item[string]{node: "C", priority: 4})
		heap.Push(pq, &item[string]{node: "D", priority: 2})

		if pq.Len() != 4 {
			t.Errorf("Queue length should be 4, got %d", pq.Len())
		}

		// Test pop order
		expectedOrder := []struct {
			node     string
			priority int
		}{
			{"B", 1},
			{"D", 2},
			{"A", 3},
			{"C", 4},
		}

		for _, expected := range expectedOrder {
			got := heap.Pop(pq).(*item[string])
			if got.node != expected.node || got.priority != expected.priority {
				t.Errorf("Expected %v:%d, got %v:%d",
					expected.node, expected.priority,
					got.node, got.priority)
			}
		}

		if pq.Len() != 0 {
			t.Errorf("Queue should be empty after pops, got length %d", pq.Len())
		}
	})

	t.Run("heap invariant", func(t *testing.T) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)

		// AddCleanup items in non-sorted order
		heap.Push(pq, &item[string]{node: "A", priority: 5})
		heap.Push(pq, &item[string]{node: "B", priority: 3})
		heap.Push(pq, &item[string]{node: "C", priority: 1})
		heap.Push(pq, &item[string]{node: "D", priority: 4})
		heap.Push(pq, &item[string]{node: "E", priority: 2})

		// Verify heap property: parent's priority <= children's priorities
		for i := 0; i < pq.Len(); i++ {
			parentPriority := pq.items[i].priority
			leftChild := 2*i + 1
			rightChild := 2*i + 2

			if leftChild < pq.Len() {
				if pq.items[leftChild].priority < parentPriority {
					t.Errorf("Heap property violated at index %d", i)
				}
			}
			if rightChild < pq.Len() {
				if pq.items[rightChild].priority < parentPriority {
					t.Errorf("Heap property violated at index %d", i)
				}
			}
		}
	})

	t.Run("edge cases", func(t *testing.T) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)

		// Test same priority
		heap.Push(pq, &item[string]{node: "A", priority: 1})
		heap.Push(pq, &item[string]{node: "B", priority: 1})
		heap.Push(pq, &item[string]{node: "C", priority: 1})

		if pq.Len() != 3 {
			t.Errorf("Expected length 3, got %d", pq.Len())
		}

		// Test negative priorities
		pq = &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)
		heap.Push(pq, &item[string]{node: "A", priority: -1})
		heap.Push(pq, &item[string]{node: "B", priority: -2})
		heap.Push(pq, &item[string]{node: "C", priority: -3})

		first := heap.Pop(pq).(*item[string])
		if first.priority != -3 {
			t.Errorf("Expected priority -3, got %d", first.priority)
		}
	})
}

func TestPriorityQueueBasicOperations(t *testing.T) {
	t.Run("push and pop operations", func(t *testing.T) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)

		// Test empty queue
		if pq.Len() != 0 {
			t.Errorf("New queue should be empty, got length %d", pq.Len())
		}

		// AddCleanup items using heap.Push
		heap.Push(pq, &item[string]{node: "A", priority: 1})
		heap.Push(pq, &item[string]{node: "B", priority: 2})

		// Verify order with heap.Pop
		first := heap.Pop(pq).(*item[string])
		if first.node != "A" || first.priority != 1 {
			t.Errorf("Pop got %v:%d, want A:1", first.node, first.priority)
		}

		second := heap.Pop(pq).(*item[string])
		if second.node != "B" || second.priority != 2 {
			t.Errorf("Pop got %v:%d, want B:2", second.node, second.priority)
		}

		// Verify empty queue behavior
		if pq.Len() != 0 {
			t.Error("Queue should be empty after popping all items")
		}
	})
}

func TestPriorityQueueUpdatePriority(t *testing.T) {
	t.Run("update priority", func(t *testing.T) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)

		// AddCleanup items
		itemA := &item[string]{node: "A", priority: 3}
		itemB := &item[string]{node: "B", priority: 2}
		itemC := &item[string]{node: "C", priority: 1}

		heap.Push(pq, itemA)
		heap.Push(pq, itemB)
		heap.Push(pq, itemC)

		// Update B's priority to highest (lowest number)
		itemB.priority = 0
		heap.Fix(pq, itemB.index)

		// Verify new order
		first := heap.Pop(pq).(*item[string])
		if first.node != "B" || first.priority != 0 {
			t.Errorf("AddCleanup priority update, expected B:0, got %v:%d", first.node, first.priority)
		}

		second := heap.Pop(pq).(*item[string])
		if second.node != "C" || second.priority != 1 {
			t.Errorf("Expected C:1 second, got %v:%d", second.node, second.priority)
		}
	})
}

func BenchmarkPriorityQueueOperations(b *testing.B) {
	b.Run("push", func(b *testing.B) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			heap.Push(pq, &item[string]{
				node:     string(rune('A' + i%26)),
				priority: i,
			})
		}
	})

	b.Run("pop", func(b *testing.B) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)

		// Setup initial items
		for i := 0; i < b.N; i++ {
			heap.Push(pq, &item[string]{
				node:     string(rune('A' + i%26)),
				priority: i,
			})
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if pq.Len() > 0 {
				heap.Pop(pq)
			}
		}
	})

	b.Run("mixed", func(b *testing.B) {
		pq := &priorityQueue[string]{items: make([]*item[string], 0)}
		heap.Init(pq)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			if i%2 == 0 {
				heap.Push(pq, &item[string]{
					node:     string(rune('A' + i%26)),
					priority: i,
				})
			} else if pq.Len() > 0 {
				heap.Pop(pq)
			}
		}
	})
}

func TestPriorityQueueGenerics(t *testing.T) {
	t.Run("integer nodes", func(t *testing.T) {
		pq := &priorityQueue[int]{items: make([]*item[int], 0)}
		heap.Init(pq)

		heap.Push(pq, &item[int]{node: 1, priority: 3})
		heap.Push(pq, &item[int]{node: 2, priority: 1})
		heap.Push(pq, &item[int]{node: 3, priority: 2})

		// Verify pop order
		expected := []struct {
			node     int
			priority int
		}{
			{2, 1},
			{3, 2},
			{1, 3},
		}

		for _, exp := range expected {
			got := heap.Pop(pq).(*item[int])
			if got.node != exp.node || got.priority != exp.priority {
				t.Errorf("Expected %v:%d, got %v:%d",
					exp.node, exp.priority,
					got.node, got.priority)
			}
		}
	})

	t.Run("custom type nodes", func(t *testing.T) {
		type CustomID struct {
			id string
		}
		pq := &priorityQueue[CustomID]{items: make([]*item[CustomID], 0)}
		heap.Init(pq)

		nodes := []CustomID{{id: "A"}, {id: "B"}, {id: "C"}}
		for i, node := range nodes {
			heap.Push(pq, &item[CustomID]{node: node, priority: i})
		}

		first := heap.Pop(pq).(*item[CustomID])
		if first.node.id != "A" || first.priority != 0 {
			t.Errorf("Expected A:0, got %v:%d", first.node.id, first.priority)
		}
	})
}

func TestPriorityQueueSwap(t *testing.T) {
	pq := &priorityQueue[string]{items: make([]*item[string], 0)}
	heap.Init(pq)

	// AddCleanup items
	heap.Push(pq, &item[string]{node: "A", priority: 1})
	heap.Push(pq, &item[string]{node: "B", priority: 2})

	// Test swap
	pq.Swap(0, 1)

	// Verify items were swapped correctly
	if pq.items[0].node != "B" || pq.items[1].node != "A" {
		t.Error("Swap did not correctly exchange items")
	}

	// Verify indices were updated
	if pq.items[0].index != 0 || pq.items[1].index != 1 {
		t.Error("Swap did not correctly update indices")
	}
}
