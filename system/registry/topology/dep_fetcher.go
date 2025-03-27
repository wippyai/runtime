package topology

import (
	"github.com/ponyruntime/pony/api/registry"
	"sync"
)

// DepFetcherHandler is a function that extracts dependencies from entry data
// It takes any data and returns a slice of dependency strings
// Each handler decides whether it can handle the given data
type DepFetcherHandler func(data any) []string

var (
	// handlers stores all registered dependency fetcher handlers
	handlers     []DepFetcherHandler
	handlersLock sync.RWMutex
)

// RegisterDepFetcherHandler registers a handler for extracting dependencies
// The handler should check internally if it can process the given data type
func RegisterDepFetcherHandler(handler DepFetcherHandler) {
	if handler == nil {
		return
	}

	handlersLock.Lock()
	defer handlersLock.Unlock()

	handlers = append(handlers, handler)
}

// fetchDependencies extracts dependencies from an entry by calling
// all registered handlers and collecting their results
func fetchDependencies(entry registry.Entry) []string {
	data := entry.Data.Data()
	if data == nil {
		return nil
	}

	handlersLock.RLock()
	defer handlersLock.RUnlock()

	var allDeps []string

	// Call all registered handlers
	for _, handler := range handlers {
		deps := handler(data)
		if len(deps) > 0 {
			allDeps = append(allDeps, deps...)
		}
	}

	return allDeps
}

// ClearDepFetcherHandlers removes all registered handlers
// Useful for testing or reinitialization
func ClearDepFetcherHandlers() {
	handlersLock.Lock()
	defer handlersLock.Unlock()

	handlers = nil
}
