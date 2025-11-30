package clock

import (
	"container/heap"
	"container/list"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// wheelTimer represents a timer in the wheel.
type wheelTimer struct {
	expiration int64 // atomic access required
	callback   func()
	firedC     chan time.Time

	// mu protects bucket membership fields
	mu      sync.Mutex
	b       *wheelBucket
	element *list.Element

	stopped atomic.Bool
}

func (t *wheelTimer) getBucket() *wheelBucket {
	t.mu.Lock()
	b := t.b
	t.mu.Unlock()
	return b
}

func (t *wheelTimer) setBucket(b *wheelBucket) {
	t.mu.Lock()
	t.b = b
	t.mu.Unlock()
}

func (t *wheelTimer) getExpiration() int64 {
	return atomic.LoadInt64(&t.expiration)
}

func (t *wheelTimer) setExpiration(exp int64) {
	atomic.StoreInt64(&t.expiration, exp)
}

// wheelBucket holds timers at the same time slot.
type wheelBucket struct {
	expiration int64 // bucket expiration time in ms
	mu         sync.Mutex
	timers     *list.List

	// heap index for delayqueue, -1 means not in heap
	index int
}

func newWheelBucket() *wheelBucket {
	return &wheelBucket{
		expiration: -1,
		timers:     list.New(),
		index:      -1,
	}
}

// SetExpiration sets bucket expiration. Returns true if changed.
func (b *wheelBucket) SetExpiration(exp int64) bool {
	return atomic.SwapInt64(&b.expiration, exp) != exp
}

func (b *wheelBucket) Expiration() int64 {
	return atomic.LoadInt64(&b.expiration)
}

func (b *wheelBucket) Add(t *wheelTimer) {
	b.mu.Lock()
	t.mu.Lock()
	e := b.timers.PushBack(t)
	t.element = e
	t.b = b
	t.mu.Unlock()
	b.mu.Unlock()
}

// remove is the internal removal that requires holding both b.mu and t.mu
func (b *wheelBucket) remove(t *wheelTimer) bool {
	// Caller must hold b.mu
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.b != b {
		return false
	}
	if t.element != nil {
		b.timers.Remove(t.element)
		t.element = nil
	}
	t.b = nil
	return true
}

// Remove removes a timer from its current bucket.
func (b *wheelBucket) Remove(t *wheelTimer) bool {
	// First get current bucket with timer lock
	t.mu.Lock()
	bucket := t.b
	t.mu.Unlock()

	if bucket == nil {
		return false
	}

	// Lock bucket, then re-check timer
	bucket.mu.Lock()
	t.mu.Lock()
	if t.b != bucket {
		t.mu.Unlock()
		bucket.mu.Unlock()
		// Bucket changed, retry with new bucket
		return b.Remove(t)
	}
	if t.element != nil {
		bucket.timers.Remove(t.element)
		t.element = nil
	}
	t.b = nil
	t.mu.Unlock()
	bucket.mu.Unlock()
	return true
}

// Flush removes all timers and returns them for reinsertion.
func (b *wheelBucket) Flush() []*wheelTimer {
	b.mu.Lock()
	defer b.mu.Unlock()

	result := make([]*wheelTimer, 0, b.timers.Len())
	for e := b.timers.Front(); e != nil; {
		next := e.Next()
		t := e.Value.(*wheelTimer)
		t.mu.Lock()
		b.timers.Remove(e)
		t.element = nil
		t.b = nil
		t.mu.Unlock()
		result = append(result, t)
		e = next
	}
	b.SetExpiration(-1)
	return result
}

// wheelBucketHeap implements heap.Interface for buckets ordered by expiration.
type wheelBucketHeap []*wheelBucket

func (h wheelBucketHeap) Len() int           { return len(h) }
func (h wheelBucketHeap) Less(i, j int) bool { return h[i].Expiration() < h[j].Expiration() }
func (h wheelBucketHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *wheelBucketHeap) Push(x any) {
	b := x.(*wheelBucket)
	b.index = len(*h)
	*h = append(*h, b)
}

func (h *wheelBucketHeap) Pop() any {
	old := *h
	n := len(old)
	b := old[n-1]
	old[n-1] = nil
	b.index = -1
	*h = old[0 : n-1]
	return b
}

// wheelDelayQueue manages buckets with expiration times.
type wheelDelayQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	heap    wheelBucketHeap
	wakeupC chan struct{} // signals wakeup, closed on shutdown
	closed  atomic.Bool
}

func newWheelDelayQueue() *wheelDelayQueue {
	dq := &wheelDelayQueue{
		heap:    make(wheelBucketHeap, 0),
		wakeupC: make(chan struct{}, 1),
	}
	dq.cond = sync.NewCond(&dq.mu)
	return dq
}

// Offer adds or updates a bucket in the queue.
func (dq *wheelDelayQueue) Offer(b *wheelBucket) {
	dq.mu.Lock()
	if b.index == -1 {
		heap.Push(&dq.heap, b)
	} else {
		heap.Fix(&dq.heap, b.index)
	}
	dq.mu.Unlock()

	// Non-blocking signal
	select {
	case dq.wakeupC <- struct{}{}:
	default:
	}
}

// Poll blocks until a bucket expires or exit is signaled.
func (dq *wheelDelayQueue) Poll(exitC <-chan struct{}) *wheelBucket {
	for {
		select {
		case <-exitC:
			return nil
		default:
		}

		dq.mu.Lock()
		if len(dq.heap) == 0 {
			dq.mu.Unlock()
			select {
			case <-exitC:
				return nil
			case <-dq.wakeupC:
				continue
			}
		}

		b := dq.heap[0]
		now := time.Now().UnixMilli()
		delay := b.Expiration() - now

		if delay <= 0 {
			heap.Pop(&dq.heap)
			dq.mu.Unlock()
			return b
		}
		dq.mu.Unlock()

		// Wait for delay or wakeup
		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		select {
		case <-exitC:
			timer.Stop()
			return nil
		case <-dq.wakeupC:
			timer.Stop()
			// New bucket added or bucket updated, re-check
		case <-timer.C:
			// Delay elapsed, re-check
		}
	}
}

func (dq *wheelDelayQueue) Close() {
	if dq.closed.Swap(true) {
		return
	}
	// Signal wakeup to unblock Poll
	select {
	case dq.wakeupC <- struct{}{}:
	default:
	}
}

// TimingWheel is the core hierarchical timing wheel structure.
type TimingWheel struct {
	tickMs      int64
	wheelSize   int64
	interval    int64
	currentTime int64

	buckets       []*wheelBucket
	queue         *wheelDelayQueue
	overflowWheel atomic.Pointer[TimingWheel]
}

// NewTimingWheel creates a timing wheel.
func NewTimingWheel(tickMs int64, wheelSize int64, startMs int64) *TimingWheel {
	buckets := make([]*wheelBucket, wheelSize)
	for i := range buckets {
		buckets[i] = newWheelBucket()
	}

	return &TimingWheel{
		tickMs:      tickMs,
		wheelSize:   wheelSize,
		interval:    tickMs * wheelSize,
		currentTime: truncateMs(startMs, tickMs),
		buckets:     buckets,
	}
}

func truncateMs(t, tick int64) int64 {
	return t - t%tick
}

// Add adds a timer to the wheel. Returns true if added.
func (tw *TimingWheel) Add(t *wheelTimer) bool {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	expiration := t.getExpiration()

	if expiration < currentTime+tw.tickMs {
		return false
	}

	if expiration < currentTime+tw.interval {
		virtualID := expiration / tw.tickMs
		b := tw.buckets[virtualID%tw.wheelSize]
		b.Add(t)

		exp := truncateMs(expiration, tw.tickMs)
		if b.SetExpiration(exp) {
			tw.queue.Offer(b)
		}
		return true
	}

	ow := tw.overflowWheel.Load()
	if ow == nil {
		tw.addOverflowWheel(currentTime)
		ow = tw.overflowWheel.Load()
	}
	return ow.Add(t)
}

func (tw *TimingWheel) addOverflowWheel(currentTime int64) {
	newWheel := &TimingWheel{
		tickMs:      tw.interval,
		wheelSize:   tw.wheelSize,
		interval:    tw.interval * tw.wheelSize,
		currentTime: truncateMs(currentTime, tw.interval),
		buckets:     make([]*wheelBucket, tw.wheelSize),
		queue:       tw.queue,
	}
	for i := range newWheel.buckets {
		newWheel.buckets[i] = newWheelBucket()
	}
	tw.overflowWheel.CompareAndSwap(nil, newWheel)
}

func (tw *TimingWheel) advanceClock(expiration int64) {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if expiration >= currentTime+tw.tickMs {
		currentTime = truncateMs(expiration, tw.tickMs)
		atomic.StoreInt64(&tw.currentTime, currentTime)

		if ow := tw.overflowWheel.Load(); ow != nil {
			ow.advanceClock(currentTime)
		}
	}
}

const (
	wheelShardCount = 64
	wheelShardMask  = wheelShardCount - 1
)

// wheelShard is a single shard of the wheel timer registry.
type wheelShard struct {
	mu     sync.Mutex
	timers map[uint64]*wheelTimer
}

// WheelTimerRegistry manages timers using a hierarchical timing wheel.
type WheelTimerRegistry struct {
	wheel   *TimingWheel
	queue   *wheelDelayQueue
	shards  [wheelShardCount]wheelShard
	nextID  atomic.Uint64
	stopped atomic.Bool
	exitC   chan struct{}
	wg      sync.WaitGroup
}

// NewWheelTimerRegistry creates a new timing wheel based timer registry.
func NewWheelTimerRegistry() *WheelTimerRegistry {
	now := time.Now().UnixMilli()
	queue := newWheelDelayQueue()

	wheel := NewTimingWheel(1, 512, now)
	wheel.queue = queue

	r := &WheelTimerRegistry{
		wheel: wheel,
		queue: queue,
		exitC: make(chan struct{}),
	}

	for i := range r.shards {
		r.shards[i].timers = make(map[uint64]*wheelTimer, 16)
	}

	r.wg.Add(1)
	go r.run()

	return r
}

func (r *WheelTimerRegistry) getShard(id uint64) *wheelShard {
	return &r.shards[id&wheelShardMask]
}

func (r *WheelTimerRegistry) run() {
	defer r.wg.Done()

	for {
		b := r.queue.Poll(r.exitC)
		if b == nil {
			return
		}

		r.wheel.advanceClock(b.Expiration())

		timers := b.Flush()
		for _, t := range timers {
			if t.stopped.Load() {
				continue
			}
			if !r.wheel.Add(t) {
				r.fireTimer(t)
			}
		}
	}
}

func (r *WheelTimerRegistry) fireTimer(t *wheelTimer) {
	if t.callback != nil {
		go t.callback()
	}
	select {
	case t.firedC <- time.Now():
	default:
	}
}

// Start creates a new timer that will fire after duration d.
func (r *WheelTimerRegistry) Start(d time.Duration) uint64 {
	id := r.nextID.Add(1)

	t := &wheelTimer{
		firedC: make(chan time.Time, 1),
	}
	t.setExpiration(time.Now().UnixMilli() + d.Milliseconds())

	shard := r.getShard(id)
	shard.mu.Lock()
	shard.timers[id] = t
	shard.mu.Unlock()

	if !r.wheel.Add(t) {
		r.fireTimer(t)
	}

	return id
}

// StartWithCallback creates a timer with an immediate callback.
func (r *WheelTimerRegistry) StartWithCallback(d time.Duration, callback func()) uint64 {
	id := r.nextID.Add(1)

	t := &wheelTimer{
		callback: callback,
		firedC:   make(chan time.Time, 1),
	}
	t.setExpiration(time.Now().UnixMilli() + d.Milliseconds())

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

	if !ok || t.stopped.Load() {
		return time.Time{}, ErrTimerNotFound
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case fireTime := <-t.firedC:
		shard.mu.Lock()
		delete(shard.timers, id)
		shard.mu.Unlock()
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
	if b := t.getBucket(); b != nil {
		b.Remove(t)
	}

	return stopped, nil
}

// Reset resets a timer to fire after a new duration.
func (r *WheelTimerRegistry) Reset(id uint64, d time.Duration) (bool, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	t, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok || t.stopped.Load() {
		return false, ErrTimerNotFound
	}

	// Remove from current bucket if any
	if b := t.getBucket(); b != nil {
		b.Remove(t)
	}

	if t.stopped.Load() {
		return false, ErrTimerNotFound
	}

	t.setExpiration(time.Now().UnixMilli() + d.Milliseconds())

	if !r.wheel.Add(t) {
		r.fireTimer(t)
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
	r.queue.Close()
	r.wg.Wait()

	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id := range shard.timers {
			delete(shard.timers, id)
		}
		shard.mu.Unlock()
	}
}
