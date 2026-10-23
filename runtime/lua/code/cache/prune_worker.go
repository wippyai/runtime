// SPDX-License-Identifier: MPL-2.0

package cache

import "sync"

// pruneWorker coalesces requests and runs one cleanup pass at a time.
// It exits when idle; writes during a pass request one further pass.
type pruneWorker struct {
	prune   func() error
	onError func(error)
	mu      sync.Mutex
	done    sync.WaitGroup
	running bool
	pending bool
}

func (w *pruneWorker) request() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = true
	if w.running {
		return
	}
	w.running = true
	w.done.Add(1)
	go w.run()
}

func (w *pruneWorker) run() {
	defer w.done.Done()
	for {
		w.mu.Lock()
		w.pending = false
		w.mu.Unlock()

		err := w.prune()
		w.mu.Lock()
		handler := w.onError
		w.mu.Unlock()
		if err != nil && handler != nil {
			handler(err)
		}

		w.mu.Lock()
		if !w.pending {
			w.running = false
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
	}
}

func (w *pruneWorker) setErrorHandler(fn func(error)) {
	w.mu.Lock()
	w.onError = fn
	w.mu.Unlock()
}

// wait is called after the writes whose requests it should cover have returned.
func (w *pruneWorker) wait() {
	w.done.Wait()
}
