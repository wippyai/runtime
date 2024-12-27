package graph

import (
	"testing"
)

func TestPriorityQueue(t *testing.T) {
	t.Run("Basic operations", func(t *testing.T) {
		pq := newPriorityQueue()

		// Test empty queue
		if pq.Len() != 0 {
			t.Errorf("New queue should be empty, got length %d", pq.Len())
		}
		if pq.Peek() != nil {
			t.Error("Peek on empty queue should return nil")
		}
		if pq.SafePop() != nil {
			t.Error("SafePop on empty queue should return nil")
		}

		// Test pushing items
		items := []*item{
			newItem("A", 3),
			newItem("B", 1),
			newItem("C", 4),
			newItem("D", 2),
		}

		for _, item := range items {
			pq.SafePush(item)
		}

		if pq.Len() != 4 {
			t.Errorf("Queue length should be 4, got %d", pq.Len())
		}

		// Test peek
		if peek := pq.Peek(); peek.node != "B" || peek.priority != 1 {
			t.Errorf("Peek should return B:1, got %v:%d", peek.node, peek.priority)
		}

		// Test pop order
		expectedOrder := []struct {
			node     Node
			priority int
		}{
			{"B", 1},
			{"D", 2},
			{"A", 3},
			{"C", 4},
		}

		for _, expected := range expectedOrder {
			item := pq.SafePop()
			if item == nil {
				t.Fatal("Unexpected nil item")
			}
			if item.node != expected.node || item.priority != expected.priority {
				t.Errorf("Expected %v:%d, got %v:%d",
					expected.node, expected.priority,
					item.node, item.priority)
			}
		}

		if pq.Len() != 0 {
			t.Errorf("Queue should be empty after pops, got length %d", pq.Len())
		}
	})

	t.Run("Update priority", func(t *testing.T) {
		pq := newPriorityQueue()

		// Add items
		itemA := newItem("A", 3)
		itemB := newItem("B", 2)
		itemC := newItem("C", 1)

		pq.SafePush(itemA)
		pq.SafePush(itemB)
		pq.SafePush(itemC)

		// Update B to highest priority
		pq.UpdatePriority(itemB, 0)

		// Verify new order
		expected := []Node{"B", "C", "A"}
		for _, expectedNode := range expected {
			item := pq.SafePop()
			if item.node != expectedNode {
				t.Errorf("Expected node %v, got %v", expectedNode, item.node)
			}
		}
	})

	t.Run("Contains", func(t *testing.T) {
		pq := newPriorityQueue()

		// Add items
		pq.SafePush(newItem("A", 1))
		pq.SafePush(newItem("B", 2))

		// Test existing nodes
		if item := pq.Contains("A"); item == nil {
			t.Error("Contains failed to find existing node A")
		}
		if item := pq.Contains("B"); item == nil {
			t.Error("Contains failed to find existing node B")
		}

		// Test non-existent node
		if item := pq.Contains("C"); item != nil {
			t.Error("Contains found non-existent node C")
		}
	})

	t.Run("Heap invariant", func(t *testing.T) {
		pq := newPriorityQueue()

		// Add items in non-sorted order
		items := []struct {
			node     Node
			priority int
		}{
			{"A", 5},
			{"B", 3},
			{"C", 1},
			{"D", 4},
			{"E", 2},
		}

		for _, item := range items {
			pq.SafePush(newItem(item.node, item.priority))
		}

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

	t.Run("Edge cases", func(t *testing.T) {
		pq := newPriorityQueue()

		// Test same priority
		pq.SafePush(newItem("A", 1))
		pq.SafePush(newItem("B", 1))
		pq.SafePush(newItem("C", 1))

		if pq.Len() != 3 {
			t.Errorf("Expected length 3, got %d", pq.Len())
		}

		// Test negative priorities
		pq = newPriorityQueue()
		pq.SafePush(newItem("A", -1))
		pq.SafePush(newItem("B", -2))
		pq.SafePush(newItem("C", -3))

		item := pq.SafePop()
		if item.priority != -3 {
			t.Errorf("Expected priority -3, got %d", item.priority)
		}

		// Test large priorities
		pq = newPriorityQueue()
		pq.SafePush(newItem("A", 1000000))
		pq.SafePush(newItem("B", 999999))

		item = pq.SafePop()
		if item.node != "B" {
			t.Errorf("Expected node B, got %v", item.node)
		}
	})

	t.Run("Multiple operations", func(t *testing.T) {
		pq := newPriorityQueue()

		// Push items
		pq.SafePush(newItem("A", 3))
		pq.SafePush(newItem("B", 1))

		// Pop highest priority
		item := pq.SafePop()
		if item.node != "B" {
			t.Errorf("Expected B, got %v", item.node)
		}

		// Push more items
		pq.SafePush(newItem("C", 2))
		pq.SafePush(newItem("D", 4))

		// Update existing item's priority
		itemA := pq.Contains("A")
		if itemA == nil {
			t.Fatal("Failed to find item A")
		}
		pq.UpdatePriority(itemA, 5)

		// Verify final order
		expected := []Node{"C", "D", "A"}
		for _, expectedNode := range expected {
			item := pq.SafePop()
			if item.node != expectedNode {
				t.Errorf("Expected node %v, got %v", expectedNode, item.node)
			}
		}
	})
}

func TestItem(t *testing.T) {
	t.Run("New item", func(t *testing.T) {
		item := newItem("test", 42)

		if item.node != "test" {
			t.Errorf("Expected node 'test', got %v", item.node)
		}
		if item.priority != 42 {
			t.Errorf("Expected priority 42, got %d", item.priority)
		}
		if item.index != -1 {
			t.Errorf("Expected index -1, got %d", item.index)
		}
	})
}

func BenchmarkPriorityQueue(b *testing.B) {
	b.Run("Push operations", func(b *testing.B) {
		pq := newPriorityQueue()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			pq.SafePush(newItem(Node(string(rune('A'+i%26))), i))
		}
	})

	b.Run("Pop operations", func(b *testing.B) {
		pq := newPriorityQueue()
		for i := 0; i < b.N; i++ {
			pq.SafePush(newItem(Node(string(rune('A'+i%26))), i))
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pq.SafePop()
		}
	})

	b.Run("Mixed operations", func(b *testing.B) {
		pq := newPriorityQueue()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			if i%2 == 0 {
				pq.SafePush(newItem(Node(string(rune('A'+i%26))), i))
			} else if pq.Len() > 0 {
				pq.SafePop()
			}
		}
	})
}
