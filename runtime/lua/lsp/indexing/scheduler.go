package indexing

import (
	"context"
	"runtime"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Scheduler manages dependency-ordered, parallel indexing.
type Scheduler struct {
	provider   Provider
	ctx        context.Context
	idleCh     chan struct{}
	deps       map[registry.ID]map[registry.ID]bool
	idx        *Indexer
	workCh     chan registry.ID
	queued     map[registry.ID]bool
	inFlight   map[registry.ID]bool
	dirty      map[registry.ID]bool
	indegree   map[registry.ID]int
	dependents map[registry.ID]map[registry.ID]bool
	doneCh     chan registry.ID
	enqueueCh  chan []registry.ID
	readySet   map[registry.ID]bool
	log        *zap.Logger
	ready      []registry.ID
	wg         sync.WaitGroup
	inCount    int
	workers    int
	mu         sync.Mutex
	enabled    bool
}

// NewScheduler constructs a scheduler with a worker pool.
func NewScheduler(log *zap.Logger, idx *Indexer, provider Provider, workers int) *Scheduler {
	if log == nil {
		log = zap.NewNop()
	}
	if workers < 1 {
		workers = runtime.GOMAXPROCS(0)
		if workers < 1 {
			workers = 1
		}
	}

	enabled := idx != nil && provider != nil

	return &Scheduler{
		log:        log.Named("indexer"),
		idx:        idx,
		provider:   provider,
		workers:    workers,
		enabled:    enabled,
		queued:     make(map[registry.ID]bool),
		inFlight:   make(map[registry.ID]bool),
		dirty:      make(map[registry.ID]bool),
		indegree:   make(map[registry.ID]int),
		dependents: make(map[registry.ID]map[registry.ID]bool),
		deps:       make(map[registry.ID]map[registry.ID]bool),
		readySet:   make(map[registry.ID]bool),
	}
}

// Start begins scheduler goroutines.
func (s *Scheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.ctx = ctx
	if !s.enabled {
		return
	}

	s.enqueueCh = make(chan []registry.ID, 16)
	s.doneCh = make(chan registry.ID, s.workers)
	s.workCh = make(chan registry.ID, s.workers)

	s.wg.Add(1)
	go s.run()

	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
}

// Stop waits for workers to exit.
func (s *Scheduler) Stop() {
	if s == nil || !s.enabled {
		return
	}
	s.wg.Wait()
}

// Enqueue schedules IDs asynchronously.
func (s *Scheduler) Enqueue(ids []registry.ID) {
	if s == nil || !s.enabled || len(ids) == 0 {
		return
	}
	if s.enqueueCh == nil {
		s.EnqueueSync(ids)
		return
	}
	select {
	case s.enqueueCh <- ids:
	case <-s.ctx.Done():
	}
}

// EnqueueAll schedules all Lua entries.
func (s *Scheduler) EnqueueAll() {
	if s == nil || !s.enabled || s.idx == nil {
		return
	}
	entries := s.idx.collectLuaEntries()
	if len(entries) == 0 {
		return
	}
	ids := make([]registry.ID, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	s.Enqueue(ids)
}

// EnqueueAllSync schedules all entries and applies immediately.
func (s *Scheduler) EnqueueAllSync() {
	if s == nil || !s.enabled || s.idx == nil {
		return
	}
	entries := s.idx.collectLuaEntries()
	if len(entries) == 0 {
		return
	}
	ids := make([]registry.ID, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	s.EnqueueSync(ids)
}

// EnqueueSync schedules IDs and applies immediately in the caller goroutine.
func (s *Scheduler) EnqueueSync(ids []registry.ID) {
	if s == nil || !s.enabled || len(ids) == 0 {
		return
	}
	s.mu.Lock()
	s.enqueueLocked(ids)
	s.dispatchLocked()
	s.signalIdleLocked()
	s.mu.Unlock()
}

// WaitIdle blocks until all queued work completes.
func (s *Scheduler) WaitIdle(ctx context.Context) error {
	if s == nil || !s.enabled {
		return nil
	}
	s.mu.Lock()
	if len(s.queued) == 0 && s.inCount == 0 {
		s.mu.Unlock()
		return nil
	}
	if s.idleCh == nil {
		s.idleCh = make(chan struct{})
	}
	ch := s.idleCh
	s.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) run() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case ids := <-s.enqueueCh:
			s.mu.Lock()
			s.enqueueLocked(ids)
			s.dispatchLocked()
			s.signalIdleLocked()
			s.mu.Unlock()
		case id := <-s.doneCh:
			s.mu.Lock()
			s.completeLocked(id)
			s.dispatchLocked()
			s.signalIdleLocked()
			s.mu.Unlock()
		}
	}
}

func (s *Scheduler) worker() {
	defer s.wg.Done()
	if s.idx == nil || s.provider == nil {
		return
	}

	var checker *lspChecker
	var builtinHash string

	for {
		select {
		case <-s.ctx.Done():
			return
		case id := <-s.workCh:
			currentHash := s.provider.BuiltinManifestHash()
			if checker == nil || currentHash != builtinHash {
				checker = s.idx.newChecker()
				if checker != nil {
					builtinHash = checker.builtinHash
				} else {
					builtinHash = ""
				}
			}

			entry, err := s.idx.entryInfoForID(id)
			if err != nil {
				s.log.Debug("indexer failed to load entry", zap.String("id", id.String()), zap.Error(err))
				s.signalDone(id)
				continue
			}
			if entry == nil || checker == nil {
				s.signalDone(id)
				continue
			}

			if err := s.idx.indexEntryWithChecker(entry, checker); err != nil {
				s.log.Debug("index failed", zap.String("id", id.String()), zap.Error(err))
			}
			checker.ClearCache()
			s.signalDone(id)
		}
	}
}

func (s *Scheduler) signalDone(id registry.ID) {
	select {
	case s.doneCh <- id:
	case <-s.ctx.Done():
	}
}

func (s *Scheduler) enqueueLocked(ids []registry.ID) {
	if s.idx == nil || s.provider == nil {
		return
	}
	if s.idleCh == nil {
		s.idleCh = make(chan struct{})
	}

	toRecalc := make([]registry.ID, 0, len(ids))
	for _, id := range ids {
		if !s.queued[id] {
			entry, err := s.idx.entryInfoForID(id)
			if err != nil || entry == nil {
				continue
			}
			s.queued[id] = true
		}

		if s.inFlight[id] {
			s.dirty[id] = true
		}

		if s.queued[id] {
			toRecalc = append(toRecalc, id)
		}
	}

	for _, id := range toRecalc {
		s.recalcDepsLocked(id)
	}

	s.breakCyclesLocked()
}

func (s *Scheduler) recalcDepsLocked(id registry.ID) {
	if !s.queued[id] {
		return
	}

	newDeps := make(map[registry.ID]bool)
	deps, err := s.provider.DirectDependencies(id)
	if err == nil {
		for _, dep := range deps {
			if dep == id {
				continue
			}
			if !s.queued[dep] {
				continue
			}
			newDeps[dep] = true
		}
	}

	oldDeps := s.deps[id]

	for dep := range oldDeps {
		if !newDeps[dep] {
			s.removeDependentLocked(dep, id)
			if s.indegree[id] > 0 {
				s.indegree[id]--
			}
		}
	}

	for dep := range newDeps {
		if oldDeps != nil && oldDeps[dep] {
			continue
		}
		s.addDependentLocked(dep, id)
		s.indegree[id]++
	}

	if len(newDeps) == 0 {
		delete(s.deps, id)
	} else {
		s.deps[id] = newDeps
	}

	if s.indegree[id] == 0 {
		if !s.inFlight[id] {
			s.addReadyLocked(id)
		}
	} else {
		s.removeReadyLocked(id)
	}
}

func (s *Scheduler) addDependentLocked(dep registry.ID, id registry.ID) {
	if s.dependents[dep] == nil {
		s.dependents[dep] = make(map[registry.ID]bool)
	}
	s.dependents[dep][id] = true
}

func (s *Scheduler) removeDependentLocked(dep registry.ID, id registry.ID) {
	deps := s.dependents[dep]
	if deps == nil {
		return
	}
	delete(deps, id)
	if len(deps) == 0 {
		delete(s.dependents, dep)
	}
}

func (s *Scheduler) removeDepLocked(id registry.ID, dep registry.ID) {
	deps := s.deps[id]
	if deps == nil {
		return
	}
	delete(deps, dep)
	if len(deps) == 0 {
		delete(s.deps, id)
	}
}

func (s *Scheduler) clearDepsLocked(id registry.ID) {
	deps := s.deps[id]
	for dep := range deps {
		s.removeDependentLocked(dep, id)
	}
	delete(s.deps, id)
}

func (s *Scheduler) completeLocked(id registry.ID) {
	if !s.queued[id] {
		return
	}

	delete(s.inFlight, id)
	if s.inCount > 0 {
		s.inCount--
	}

	if s.dirty[id] {
		s.dirty[id] = false
		s.recalcDepsLocked(id)
		return
	}

	for dep := range s.dependents[id] {
		if s.indegree[dep] > 0 {
			s.indegree[dep]--
		}
		s.removeDepLocked(dep, id)
		if s.indegree[dep] == 0 && !s.inFlight[dep] {
			s.addReadyLocked(dep)
		}
	}

	delete(s.dependents, id)
	s.clearDepsLocked(id)
	s.removeReadyLocked(id)
	delete(s.indegree, id)
	delete(s.dirty, id)
	delete(s.queued, id)
}

func (s *Scheduler) dispatchLocked() {
	for len(s.ready) > 0 {
		if s.workCh == nil {
			return
		}
		id := s.ready[0]
		if s.inFlight[id] {
			s.ready = s.ready[1:]
			delete(s.readySet, id)
			continue
		}
		select {
		case s.workCh <- id:
			s.ready = s.ready[1:]
			delete(s.readySet, id)
			s.inFlight[id] = true
			s.inCount++
		default:
			return
		}
	}
}

func (s *Scheduler) addReadyLocked(id registry.ID) {
	if s.readySet[id] || s.inFlight[id] {
		return
	}
	s.ready = append(s.ready, id)
	s.readySet[id] = true
}

func (s *Scheduler) removeReadyLocked(id registry.ID) {
	if !s.readySet[id] {
		return
	}
	for i, rid := range s.ready {
		if rid == id {
			copy(s.ready[i:], s.ready[i+1:])
			s.ready = s.ready[:len(s.ready)-1]
			break
		}
	}
	delete(s.readySet, id)
}

func (s *Scheduler) signalIdleLocked() {
	if s.idleCh == nil {
		return
	}
	if len(s.queued) == 0 && s.inCount == 0 {
		close(s.idleCh)
		s.idleCh = nil
	}
}

func (s *Scheduler) breakCyclesLocked() {
	if len(s.queued) == 0 {
		return
	}
	if len(s.ready) > 0 || s.inCount > 0 {
		return
	}
	for id := range s.queued {
		if s.inFlight[id] {
			continue
		}
		s.indegree[id] = 0
		s.addReadyLocked(id)
		s.log.Warn("dependency cycle detected; indexing without strict order", zap.String("id", id.String()))
		return
	}
}
