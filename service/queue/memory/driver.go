package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type queue struct {
	id       registry.ID
	opts     attrs.Attributes
	messages chan *queueapi.Message
	mu       sync.RWMutex
	closed   bool
}

type Driver struct {
	id         registry.ID
	logger     *zap.Logger
	queues     map[registry.ID]*queue
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	statusChan chan any
}

func NewDriver(id registry.ID, logger *zap.Logger) *Driver {
	ctx, cancel := context.WithCancel(context.Background())
	return &Driver{
		id:     id,
		logger: logger,
		queues: make(map[registry.ID]*queue),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (d *Driver) Publish(ctx context.Context, queueID registry.ID, msgs ...*queueapi.Message) error {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return queueapi.ErrNoQueue
	}

	q.mu.RLock()
	closed := q.closed
	q.mu.RUnlock()

	if closed {
		return fmt.Errorf("queue %s is closed", queueID)
	}

	for _, msg := range msgs {
		select {
		case q.messages <- msg:
		case <-ctx.Done():
			return ctx.Err()
		case <-d.ctx.Done():
			return fmt.Errorf("driver is stopped")
		}
	}

	return nil
}

func (d *Driver) Attach(ctx context.Context, queueID registry.ID, deliveries chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrNoQueue
	}

	consumerCtx, cancel := context.WithCancel(ctx)

	go func() {
		for {
			select {
			case <-consumerCtx.Done():
				return
			case <-d.ctx.Done():
				return
			case msg, ok := <-q.messages:
				if !ok {
					return
				}

				delivery := &queueapi.Delivery{
					Message: msg,
					Ack: func(ctx context.Context) error {
						return nil
					},
					Nack: func(ctx context.Context) error {
						q.mu.RLock()
						closed := q.closed
						q.mu.RUnlock()

						if closed {
							return fmt.Errorf("queue is closed, cannot requeue message")
						}

						select {
						case q.messages <- msg:
							return nil
						case <-ctx.Done():
							return ctx.Err()
						default:
							return fmt.Errorf("queue is full, cannot requeue message")
						}
					},
				}

				select {
				case deliveries <- delivery:
				case <-consumerCtx.Done():
					return
				case <-d.ctx.Done():
					return
				}
			}
		}
	}()

	return cancel, nil
}

func (d *Driver) DeclareQueue(ctx context.Context, queueID registry.ID, opts attrs.Attributes) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.queues[queueID]; exists {
		return nil
	}

	maxLength := opts.GetInt(queueapi.OptionMaxLength, 1000)

	q := &queue{
		id:       queueID,
		opts:     opts,
		messages: make(chan *queueapi.Message, maxLength),
		closed:   false,
	}

	d.queues[queueID] = q

	d.logger.Debug("queue declared",
		zap.String("driver", d.id.String()),
		zap.String("queue", queueID.String()),
		zap.Int("max_length", maxLength))

	return nil
}

func (d *Driver) GetQueueInfo(ctx context.Context, queueID registry.ID) (attrs.Attributes, error) {
	d.mu.RLock()
	q, exists := d.queues[queueID]
	d.mu.RUnlock()

	if !exists {
		return nil, queueapi.ErrNoQueue
	}

	info := attrs.NewBag()
	info.Set(queueapi.StatsMessageCount, len(q.messages))
	info.Set(queueapi.StatsReady, len(q.messages))

	return info, nil
}

func (d *Driver) Start(ctx context.Context) (<-chan any, error) {
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.logger.Info("memory driver started", zap.String("id", d.id.String()))
	d.statusChan = make(chan any, 1)
	return d.statusChan, nil
}

func (d *Driver) Stop(ctx context.Context) error {
	d.cancel()

	d.mu.Lock()
	defer d.mu.Unlock()

	for id, q := range d.queues {
		q.mu.Lock()
		q.closed = true
		close(q.messages)
		q.mu.Unlock()
		delete(d.queues, id)
	}

	close(d.statusChan)
	d.logger.Info("memory driver stopped", zap.String("id", d.id.String()))
	return nil
}
