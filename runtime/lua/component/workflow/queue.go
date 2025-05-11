package workflow

import (
	"container/list"
	"sync"

	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/engine"
)

// commandQueueKey is used to store and retrieve the command queue from UnitOfWork
var commandQueueKey = &struct{ name string }{"workflow.commandQueue"}

// CommandQueue represents a thread-safe queue of runtime commands using container/list
type CommandQueue struct {
	mu       sync.Mutex
	commands *list.List
}

// NewCommandQueue creates a new empty command queue
func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		commands: list.New(),
	}
}

// Push adds a command to the queue
func (q *CommandQueue) Push(cmd runtime.Command) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.commands.PushBack(cmd)
}

// GetAll returns a slice of all commands in the queue
func (q *CommandQueue) GetAll() []runtime.Command {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Create a slice with the exact capacity needed
	result := make([]runtime.Command, q.commands.Len())

	// Iterate through the list and populate the slice
	i := 0
	for e := q.commands.Front(); e != nil; e = e.Next() {
		result[i] = e.Value.(runtime.Command)
		i++
	}

	return result
}

// Count returns the number of commands in the queue
func (q *CommandQueue) Count() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.commands.Len()
}

// Flush empties the queue and returns all commands that were in it
func (q *CommandQueue) Flush() []runtime.Command {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get all commands
	result := make([]runtime.Command, q.commands.Len())

	i := 0
	for e := q.commands.Front(); e != nil; e = e.Next() {
		result[i] = e.Value.(runtime.Command)
		i++
	}

	// Clear the queue
	q.commands.Init()

	return result
}

// GetCommandQueue retrieves the command queue from UnitOfWork
// Creates a new queue if one doesn't exist
func GetCommandQueue(uw engine.UnitOfWork) *CommandQueue {
	if uw == nil {
		return nil
	}

	val, exists := uw.Values().Get(commandQueueKey)
	if exists {
		if queue, ok := val.(*CommandQueue); ok {
			return queue
		}
	}

	// Create a new queue if none exists
	queue := NewCommandQueue()
	uw.Values().Set(commandQueueKey, queue)
	return queue
}
