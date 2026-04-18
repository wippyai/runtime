// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"context"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	consumerapi "github.com/wippyai/runtime/api/service/queue/consumer"
	"go.uber.org/zap"
)

// Consumer is a queue consumer service that processes messages by invoking functions
type Consumer struct {
	driver       queueapi.Driver
	workerCtx    context.Context
	funcReg      function.Registry
	config       *consumerapi.Config
	logger       *zap.Logger
	deliveries   chan *queueapi.Delivery
	cancel       context.CancelFunc
	workerCancel context.CancelFunc
	statusChan   chan any
	id           registry.ID
	funcID       registry.ID
	queueID      registry.ID
	wg           sync.WaitGroup
	stopOnce     sync.Once
}

// NewConsumer creates a new consumer instance
func NewConsumer(
	id registry.ID,
	config *consumerapi.Config,
	driver queueapi.Driver,
	funcReg function.Registry,
	logger *zap.Logger,
) *Consumer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Consumer{
		id:      id,
		queueID: config.Queue,
		funcID:  config.Func,
		config:  config,
		driver:  driver,
		funcReg: funcReg,
		logger:  logger,
	}
}

// Start starts the consumer and begins processing messages
func (c *Consumer) Start(ctx context.Context) (<-chan any, error) {
	c.statusChan = make(chan any, 1)
	c.deliveries = make(chan *queueapi.Delivery, c.config.Prefetch)

	// Create worker context
	c.workerCtx, c.workerCancel = context.WithCancel(ctx)
	if fc := ctxapi.FrameFromContext(c.workerCtx); fc != nil {
		// Workers share c.workerCtx across goroutines, so freeze it once.
		fc.Seal()
	}

	// Attach to driver to receive deliveries
	opts := c.config.ConsumerOptions
	cancel, err := c.driver.Attach(ctx, c.queueID, &opts, c.deliveries)
	if err != nil {
		c.workerCancel()
		close(c.statusChan)
		return c.statusChan, err
	}
	c.cancel = cancel

	// Start worker goroutines
	for i := 0; i < c.config.Concurrency; i++ {
		c.wg.Add(1)
		go c.worker(c.workerCtx, i)
	}

	c.logger.Info("consumer started",
		zap.String("id", c.id.String()),
		zap.String("queue", c.queueID.String()),
		zap.String("func", c.funcID.String()),
		zap.Int("concurrency", c.config.Concurrency))

	return c.statusChan, nil
}

// Stop stops the consumer gracefully
func (c *Consumer) Stop(ctx context.Context) error {
	var err error
	c.stopOnce.Do(func() {
		c.logger.Info("stopping consumer",
			zap.String("id", c.id.String()),
			zap.String("queue", c.queueID.String()),
			zap.String("func", c.funcID.String()))

		// Cancel driver attachment (stops new deliveries)
		if c.cancel != nil {
			c.cancel()
		}

		// Cancel worker contexts (stops workers immediately)
		if c.workerCancel != nil {
			c.workerCancel()
		}

		// Wait for workers to finish processing current messages
		done := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			close(c.statusChan)
			c.logger.Info("consumer stopped", zap.String("id", c.id.String()))
			err = nil
		case <-ctx.Done():
			close(c.statusChan)
			c.logger.Warn("consumer stop timed out", zap.String("id", c.id.String()))
			err = ctx.Err()
		}
	})
	return err
}

// worker processes messages from the delivery channel
func (c *Consumer) worker(ctx context.Context, workerID int) {
	defer c.wg.Done()

	c.logger.Debug("worker started",
		zap.String("consumer", c.id.String()),
		zap.Int("worker_id", workerID))

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("worker stopped (context canceled)",
				zap.String("consumer", c.id.String()),
				zap.Int("worker_id", workerID))
			return

		case delivery, ok := <-c.deliveries:
			if !ok {
				c.logger.Debug("worker stopped (channel closed)",
					zap.String("consumer", c.id.String()),
					zap.Int("worker_id", workerID))
				return
			}

			c.processDelivery(ctx, delivery, workerID)
		}
	}
}

// processDelivery processes a single message delivery
func (c *Consumer) processDelivery(ctx context.Context, delivery *queueapi.Delivery, workerID int) {
	msg := delivery.Message
	defer func() {
		// Signal wrappers that outlived the handler (e.g. Lua userdata
		// retained in a closure or coroutine) to stop dereferencing the
		// pooled *Message before it goes back to sync.Pool.
		delivery.Invalidate()
		queueapi.ReleaseMessage(msg)
	}()

	c.logger.Debug("processing message",
		zap.String("consumer", c.id.String()),
		zap.String("queue", c.queueID.String()),
		zap.String("func", c.funcID.String()),
		zap.Int("worker_id", workerID),
		zap.String("message_id", msg.ID))

	// Create task for function invocation with delivery context
	task := runtime.Task{
		ID:       c.funcID,
		Payloads: payload.Payloads{msg.Body},
		Context: []ctxapi.Pair{
			queueapi.DeliveryPair(delivery),
		},
	}

	// Call function
	result, err := c.funcReg.Call(ctx, task)
	if err == nil && result != nil && result.Error != nil {
		err = result.Error
	}

	// Ack or Nack based on result. MarkSettled gates the broker call: if
	// the handler already called msg:ack()/msg:nack() via the Lua
	// wrapper, the settle slot is claimed and the consumer must skip its
	// own settle to avoid double-ack (AMQP PRECONDITION_FAILED) or
	// double-nack/visibility-timeout races.
	if err != nil {
		c.logger.Error("message processing failed",
			zap.String("consumer", c.id.String()),
			zap.String("queue", c.queueID.String()),
			zap.String("func", c.funcID.String()),
			zap.Int("worker_id", workerID),
			zap.String("message_id", msg.ID),
			zap.Error(err))

		if delivery.MarkSettled() {
			if nackErr := delivery.Nack(ctx); nackErr != nil {
				c.logger.Error("failed to nack message",
					zap.String("consumer", c.id.String()),
					zap.String("message_id", msg.ID),
					zap.Error(nackErr))
			}
		}
	} else {
		c.logger.Debug("message processed successfully",
			zap.String("consumer", c.id.String()),
			zap.Int("worker_id", workerID),
			zap.String("message_id", msg.ID))

		if delivery.MarkSettled() {
			if ackErr := delivery.Ack(ctx); ackErr != nil {
				c.logger.Error("failed to ack message",
					zap.String("consumer", c.id.String()),
					zap.String("message_id", msg.ID),
					zap.Error(ackErr))
			}
		}
	}
}
