// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Driver option keys under the "memory" sub-bag of Config.DriverOptions.
const (
	optionMaxLength = "max_length"
	defaultMaxLen   = 1000
)

type queue struct {
	cfg      *queueapi.Config
	messages chan *queueapi.Message
	id       registry.ID
	mu       sync.RWMutex
	closed   bool
}

type Driver struct {
	ctx        context.Context
	logger     *zap.Logger
	queues     map[registry.ID]*queue
	cancel     context.CancelFunc
	statusChan chan any
	id         registry.ID
	mu         sync.RWMutex
}

func NewDriver(id registry.ID, logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{
		id:     id,
		logger: logger,
		queues: make(map[registry.ID]*queue),
	}
}

func (d *Driver) Publish(ctx context.Context, queueID registry.ID, msgs ...*queueapi.Message) error {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return queueapi.ErrQueueNotFound
	}

	for _, msg := range msgs {
		if msg.ID == "" {
			msg.ID = uuid.New().String()
		}
		if err := q.send(ctx, d.lifecycleCtxDone(), msg); err != nil {
			return err
		}
	}
	return nil
}

func (q *queue) send(ctx context.Context, driverDone <-chan struct{}, msg *queueapi.Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return queuesvc.NewQueueClosedError(q.id)
	}

	select {
	case q.messages <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-driverDone:
		return queuesvc.ErrDriverNotStarted
	}
}

func (q *queue) requeue(ctx context.Context, msg *queueapi.Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return queuesvc.ErrQueueClosed
	}

	select {
	case q.messages <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return queuesvc.ErrQueueFull
	}
}

func (d *Driver) Attach(ctx context.Context, queueID registry.ID, _ *queueapi.ConsumerOptions, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}

	consumerCtx, cancel := context.WithCancel(ctx)

	go func() {
		for {
			select {
			case <-consumerCtx.Done():
				return
			case <-d.lifecycleCtxDone():
				return
			case msg, ok := <-q.messages:
				if !ok {
					return
				}

				delivery := &queueapi.Delivery{
					Message: msg,
					Ack: func(_ context.Context) error {
						return nil
					},
					Nack: func(ctx context.Context) error {
						return q.requeue(ctx, queueapi.CloneMessage(msg))
					},
				}

				select {
				case deliveries <- delivery:
				case <-consumerCtx.Done():
					return
				case <-d.lifecycleCtxDone():
					return
				}
			}
		}
	}()

	return cancel, nil
}

func (d *Driver) DeclareQueue(_ context.Context, queueID registry.ID, cfg *queueapi.Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Redeclare is an update, not a no-op: swap in the latest cfg pointer
	// so subsequent Publish / GetQueueInfo see the caller's latest options.
	// Existing queued messages survive — changing options must not silently
	// drop a backlog.
	if existing, exists := d.queues[queueID]; exists {
		existing.mu.Lock()
		existing.cfg = cfg
		existing.mu.Unlock()
		return nil
	}

	maxLength := defaultMaxLen
	if cfg != nil {
		if v := cfg.DriverBag("memory").GetInt(optionMaxLength, 0); v > 0 {
			maxLength = v
		}
	}

	q := &queue{
		id:       queueID,
		cfg:      cfg,
		messages: make(chan *queueapi.Message, maxLength),
	}

	d.queues[queueID] = q

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.Int("max_length", maxLength))

	return nil
}

func (d *Driver) GetQueueInfo(_ context.Context, queueID registry.ID) (attrs.Attributes, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrQueueNotFound
	}

	q.mu.RLock()
	msgCount := len(q.messages)
	q.mu.RUnlock()

	info := attrs.NewBag()
	info.Set(queueapi.StatsMessageCount, msgCount)
	info.Set(queueapi.StatsReady, msgCount)

	return info, nil
}

// neverClosedChan is a channel that never closes, used when driver is not started.
var neverClosedChan = make(chan struct{})

func (d *Driver) lifecycleCtxDone() <-chan struct{} {
	d.mu.RLock()
	ctx := d.ctx
	d.mu.RUnlock()
	if ctx != nil {
		return ctx.Done()
	}
	return neverClosedChan
}

func (d *Driver) Start(ctx context.Context) (<-chan any, error) {
	d.mu.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.statusChan = make(chan any, 1)
	d.mu.Unlock()

	d.logger.Info("memory driver started", zap.String("id", d.id.String()))
	return d.statusChan, nil
}

func (d *Driver) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cancel != nil {
		d.cancel()
	}

	for id, q := range d.queues {
		q.mu.Lock()
		q.closed = true
		close(q.messages)
		q.mu.Unlock()
		delete(d.queues, id)
	}

	if d.statusChan != nil {
		close(d.statusChan)
	}

	d.logger.Info("memory driver stopped", zap.String("id", d.id.String()))
	return nil
}
