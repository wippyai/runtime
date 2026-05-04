// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuecfg "github.com/wippyai/runtime/api/service/queue/queue"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type DeclarationHandler struct {
	bus      event.Bus
	queueMgr queueapi.Manager
	dtt      payload.Transcoder
	logger   *zap.Logger
}

func NewDeclarationHandler(
	bus event.Bus,
	queueMgr queueapi.Manager,
	dtt payload.Transcoder,
	logger *zap.Logger,
) *DeclarationHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DeclarationHandler{
		bus:      bus,
		queueMgr: queueMgr,
		dtt:      dtt,
		logger:   logger,
	}
}

func (h *DeclarationHandler) Add(ctx context.Context, entry registry.Entry) error {
	return h.addOrUpdateQueue(ctx, entry, "declared")
}

func (h *DeclarationHandler) Update(ctx context.Context, entry registry.Entry) error {
	if err := h.deleteQueue(ctx, entry.ID); err != nil {
		return err
	}
	return h.addOrUpdateQueue(ctx, entry, "updated")
}

func (h *DeclarationHandler) Delete(ctx context.Context, entry registry.Entry) error {
	return h.deleteQueue(ctx, entry.ID)
}

func (h *DeclarationHandler) addOrUpdateQueue(ctx context.Context, entry registry.Entry, action string) error {
	cfg, err := entryutil.DecodeEntryConfig[queuecfg.Config](ctx, h.dtt, entry)
	if err != nil {
		h.logger.Error("failed to decode queue config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return NewConfigError("failed to decode queue config", err)
	}

	if err := cfg.Validate(); err != nil {
		h.logger.Error("invalid queue config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return NewConfigError("invalid queue config", err)
	}

	if _, ok := h.queueMgr.GetDriver(cfg.Driver); !ok {
		h.logger.Error("driver not found for queue",
			zap.String("id", entry.ID.String()),
			zap.String("driver", cfg.Driver.String()))
		return NewDriverNotFoundError(cfg.Driver)
	}

	name := entry.ID.Name
	if cfg.QueueName != "" {
		name = cfg.QueueName
	}
	queue := &queueapi.Queue{
		ID:       entry.ID,
		DriverID: cfg.Driver,
		Name:     name,
		Config:   cfg,
	}

	if err := SendAndAwaitManagerAck(ctx, h.bus, event.Event{
		System: queueapi.System,
		Kind:   queueapi.Declare,
		Path:   entry.ID.String(),
		Data:   queue,
	}, "queue declare"); err != nil {
		h.logger.Error("queue declare rejected",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return err
	}

	h.logger.Info("queue "+action,
		zap.String("id", entry.ID.String()),
		zap.String("driver", cfg.Driver.String()))

	return nil
}

func (h *DeclarationHandler) deleteQueue(ctx context.Context, id registry.ID) error {
	h.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.Delete,
		Path:   id.String(),
	})

	h.logger.Info("queue deleted", zap.String("id", id.String()))

	return nil
}
