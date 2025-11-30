package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// TimingWheel implements a hierarchical timing wheel for efficient timer management.
// Based on Kafka's purgatory timing wheel design.
// Achieves O(1) timer insertion and cancellation.
type TimingWheel struct {
	tick        int64 // tick duration in nanoseconds
	wheelSize   int64
	interval    int64 // tick * wheelSize
	currentTime int64 // truncated to tick boundary

	buckets       []*bucket
	overflowWheel atomic.Pointer[TimingWheel]

	exitC chan struct{}
}

// bucket holds timers at the same time slot using intrusive linked list.
type bucket struct {
	expiration atomic.Int64
	mu         sync.Mutex
	head       *wheelTimer
	tail       *wheelTimer
}

// wheelTimer represents a timer in the wheel.
// Uses intrusive linked list to avoid list.Element allocation.
type wheelTimer struct {
	expiration atomic.Int64 // timer expiration in nanoseconds
	firedC     chan time.Time
	callback   func()
	mu         sync.Mutex // protects b, prev, next
	b          *bucket
	prev       *wheelTimer
	next       *wheelTimer
	stopped    atomic.Bool
}

var wheelTimerPool = sync.Pool{
	New: func() any {
		return &wheelTimer{
			firedC: make(chan time.Time, 1),
		}
	},
}

func acquireWheelTimer() *wheelTimer {
	return wheelTimerPool.Get().(*wheelTimer)
}

func releaseWheelTimer(t *wheelTimer) {
	select {
	case <-t.firedC:
	default:
	}
	t.expiration.Store(0)
	t.callback = nil
	t.mu.Lock()
	t.b = nil
	t.prev = nil
	t.next = nil
	t.mu.Unlock()
	t.stopped.Store(false)
	wheelTimerPool.Put(t)
}

const (
	wheelTick       = int64(time.Millisecond)
	wheelSize       = 512
	wheelInterval   = wheelTick * wheelSize
	wheelShardCount = 64
	wheelShardMask  = wheelShardCount - 1
)

// wheelShard is a single shard of the wheel timer registry.
type wheelShard struct {
	mu     sync.Mutex
	timers map[uint64]*wheelTimer
}

// WheelTimerRegistry manages timers using a timing wheel.
type WheelTimerRegistry struct {
	wheel   *TimingWheel
	shards  [wheelShardCount]wheelShard
	nextID  atomic.Uint64
	stopped atomic.Bool
	exitC   chan struct{}
	wg      sync.WaitGroup
}

func (r *WheelTimerRegistry) getShard(id uint64) *wheelShard {
	return &r.shards[id&wheelShardMask]
}

// NewTimingWheel creates a timing wheel with given tick (nanoseconds) and size.
func NewTimingWheel(tick int64, wheelSize int64, startMs int64) *TimingWheel {
	startNs := startMs * int64(time.Millisecond)
	return newTimingWheel(tick, wheelSize, startNs, nil)
}

func newTimingWheel(tick int64, wheelSize int64, startNs int64, prevExitC chan struct{}) *TimingWheel {
	buckets := make([]*bucket, wheelSize)
	for i := range buckets {
		buckets[i] = newBucket()
	}

	exitC := prevExitC
	if exitC == nil {
		exitC = make(chan struct{})
	}

	return &TimingWheel{
		tick:        tick,
		wheelSize:   wheelSize,
		interval:    tick * wheelSize,
		currentTime: truncate(startNs, tick),
		buckets:     buckets,
		exitC:       exitC,
	}
}

func newBucket() *bucket {
	b := &bucket{}
	b.expiration.Store(-1)
	return b
}

func truncate(n, tick int64) int64 {
	return n - n%tick
}

// Add adds a timer to the wheel. Returns true if added, false if already expired.
func (tw *TimingWheel) Add(t *wheelTimer) bool {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	expiration := t.expiration.Load()

	if expiration < currentTime+tw.tick {
		return false
	}

	if expiration < currentTime+tw.interval {
		virtualID := expiration / tw.tick
		b := tw.buckets[virtualID%tw.wheelSize]
		b.Add(t)

		exp := truncate(expiration, tw.tick)
		b.SetExpiration(exp)
		return true
	}

	overflowWheel := tw.overflowWheel.Load()
	if overflowWheel == nil {
		tw.createOverflowWheel(currentTime)
		overflowWheel = tw.overflowWheel.Load()
	}
	return overflowWheel.Add(t)
}

func (tw *TimingWheel) createOverflowWheel(currentTime int64) {
	newWheel := newTimingWheel(tw.interval, tw.wheelSize, currentTime, tw.exitC)
	tw.overflowWheel.CompareAndSwap(nil, newWheel)
}

func (tw *TimingWheel) advanceClock(expiration int64) {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if expiration >= currentTime+tw.tick {
		currentTime = truncate(expiration, tw.tick)
		atomic.StoreInt64(&tw.currentTime, currentTime)

		if overflowWheel := tw.overflowWheel.Load(); overflowWheel != nil {
			overflowWheel.advanceClock(currentTime)
		}
	}
}

// bucket methods with intrusive linked list

func (b *bucket) Add(t *wheelTimer) {
	t.mu.Lock()
	b.mu.Lock()
	t.b = b
	t.prev = b.tail
	t.next = nil
	if b.tail != nil {
		b.tail.next = t
	} else {
		b.head = t
	}
	b.tail = t
	b.mu.Unlock()
	t.mu.Unlock()
}

func (b *bucket) Remove(t *wheelTimer) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.b == nil {
		return false
	}

	bucket := t.b
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Double-check after acquiring bucket lock
	if t.b != bucket {
		return false
	}

	if t.prev != nil {
		t.prev.next = t.next
	} else {
		bucket.head = t.next
	}
	if t.next != nil {
		t.next.prev = t.prev
	} else {
		bucket.tail = t.prev
	}

	t.b = nil
	t.prev = nil
	t.next = nil
	return true
}

func (b *bucket) SetExpiration(expiration int64) bool {
	return b.expiration.Swap(expiration) != expiration
}

func (b *bucket) Expiration() int64 {
	return b.expiration.Load()
}

func (b *bucket) Flush(reinsert func(*wheelTimer)) {
	b.mu.Lock()
	for t := b.head; t != nil; {
		next := t.next

		// Lock timer and clear its bucket reference
		t.mu.Lock()
		t.b = nil
		t.prev = nil
		t.next = nil
		t.mu.Unlock()

		b.mu.Unlock()
		reinsert(t)
		b.mu.Lock()

		t = next
	}
	b.head = nil
	b.tail = nil
	b.expiration.Store(-1)
	b.mu.Unlock()
}

// NewWheelTimerRegistry creates a new timing wheel based timer registry.
func NewWheelTimerRegistry() *WheelTimerRegistry {
	now := time.Now().UnixNano() / int64(time.Millisecond)
	r := &WheelTimerRegistry{
		wheel: NewTimingWheel(wheelTick, wheelSize, now),
		exitC: make(chan struct{}),
	}

	for i := range r.shards {
		r.shards[i].timers = make(map[uint64]*wheelTimer, 16)
	}

	r.wg.Add(1)
	go r.run()

	return r
}

func (r *WheelTimerRegistry) run() {
	defer r.wg.Done()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.exitC:
			return
		case now := <-ticker.C:
			r.advanceAndFire(now.UnixNano())
		}
	}
}

func (r *WheelTimerRegistry) advanceAndFire(now int64) {
	r.wheel.advanceClock(now)

	for _, b := range r.wheel.buckets {
		exp := b.Expiration()
		if exp != -1 && exp <= now {
			b.Flush(func(t *wheelTimer) {
				if t.stopped.Load() {
					return
				}
				if t.expiration.Load() <= now {
					r.fireTimer(t, time.Unix(0, now))
				} else {
					r.wheel.Add(t)
				}
			})
		}
	}

	if overflowWheel := r.wheel.overflowWheel.Load(); overflowWheel != nil {
		r.advanceOverflowWheel(overflowWheel, now)
	}
}

func (r *WheelTimerRegistry) fireTimer(t *wheelTimer, fireTime time.Time) {
	if t.callback != nil {
		t.callback()
	}
	select {
	case t.firedC <- fireTime:
	default:
	}
}

func (r *WheelTimerRegistry) advanceOverflowWheel(tw *TimingWheel, now int64) {
	for _, b := range tw.buckets {
		exp := b.Expiration()
		if exp != -1 && exp <= now {
			b.Flush(func(t *wheelTimer) {
				if t.stopped.Load() {
					return
				}
				r.wheel.Add(t)
			})
		}
	}

	if overflowWheel := tw.overflowWheel.Load(); overflowWheel != nil {
		r.advanceOverflowWheel(overflowWheel, now)
	}
}

// Start creates a new timer that will fire after duration d.
func (r *WheelTimerRegistry) Start(d time.Duration) uint64 {
	id := r.nextID.Add(1)

	now := time.Now().UnixNano()
	expiration := now + int64(d)

	t := acquireWheelTimer()
	t.expiration.Store(expiration)

	shard := r.getShard(id)
	shard.mu.Lock()
	shard.timers[id] = t
	shard.mu.Unlock()

	if !r.wheel.Add(t) {
		// Timer already expired (within tick), fire immediately
		r.fireTimer(t, time.Now())
	}

	return id
}

// StartWithCallback creates a timer with an immediate callback.
func (r *WheelTimerRegistry) StartWithCallback(d time.Duration, callback func()) uint64 {
	id := r.nextID.Add(1)

	now := time.Now().UnixNano()
	expiration := now + int64(d)

	t := acquireWheelTimer()
	t.expiration.Store(expiration)
	t.callback = callback

	shard := r.getShard(id)
	shard.mu.Lock()
	shard.timers[id] = t
	shard.mu.Unlock()

	if !r.wheel.Add(t) {
		go callback()
	}

	return id
}

// Wait blocks until timer fires or context is cancelled.
func (r *WheelTimerRegistry) Wait(ctx context.Context, id uint64) (time.Time, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	t, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok {
		return time.Time{}, ErrTimerNotFound
	}

	if t.stopped.Load() {
		return time.Time{}, ErrTimerNotFound
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case fireTime := <-t.firedC:
		shard.mu.Lock()
		delete(shard.timers, id)
		shard.mu.Unlock()
		releaseWheelTimer(t)
		return fireTime, nil
	}
}

// Stop cancels a timer.
func (r *WheelTimerRegistry) Stop(id uint64) (bool, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	t, ok := shard.timers[id]
	if ok {
		delete(shard.timers, id)
	}
	shard.mu.Unlock()

	if !ok {
		return false, ErrTimerNotFound
	}

	stopped := !t.stopped.Swap(true)

	// Remove from bucket (uses timer's mutex internally)
	t.mu.Lock()
	bucket := t.b
	t.mu.Unlock()
	if bucket != nil {
		bucket.Remove(t)
	}

	releaseWheelTimer(t)
	return stopped, nil
}

// Reset resets a timer to fire after a new duration.
func (r *WheelTimerRegistry) Reset(id uint64, d time.Duration) (bool, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	t, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok {
		return false, ErrTimerNotFound
	}

	if t.stopped.Load() {
		return false, nil
	}

	// Remove from current bucket (uses timer's mutex internally)
	t.mu.Lock()
	bucket := t.b
	t.mu.Unlock()
	if bucket != nil {
		bucket.Remove(t)
	}

	// Update expiration and re-add to wheel
	now := time.Now().UnixNano()
	t.expiration.Store(now + int64(d))

	if !r.wheel.Add(t) {
		// Timer already expired, fire immediately
		r.fireTimer(t, time.Now())
	}

	return true, nil
}

// Count returns the number of active timers.
func (r *WheelTimerRegistry) Count() int {
	count := 0
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		count += len(shard.timers)
		shard.mu.Unlock()
	}
	return count
}

// Close shuts down the timing wheel.
func (r *WheelTimerRegistry) Close() {
	if r.stopped.Swap(true) {
		return
	}
	close(r.exitC)
	r.wg.Wait()

	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id, t := range shard.timers {
			delete(shard.timers, id)
			releaseWheelTimer(t)
		}
		shard.mu.Unlock()
	}
}
