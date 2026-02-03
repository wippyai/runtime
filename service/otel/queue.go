package otel

import (
	"context"

	attrsapi "github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MessageHeaderCarrier adapts attrsapi.Bag to propagation.TextMapCarrier
type MessageHeaderCarrier struct {
	headers attrsapi.Bag
}

// Get retrieves a header value as string
func (c *MessageHeaderCarrier) Get(key string) string {
	v, ok := c.headers.Get(key)
	if !ok {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	return ""
}

// Set stores a header value
func (c *MessageHeaderCarrier) Set(key, value string) {
	c.headers.Set(key, value)
}

// Keys returns all header keys
func (c *MessageHeaderCarrier) Keys() []string {
	return c.headers.Keys()
}

// PublishInterceptor injects trace context into outgoing messages
type PublishInterceptor struct {
	tracer trace.Tracer
}

// NewPublishInterceptor creates a new publish interceptor
func NewPublishInterceptor(tracer trace.Tracer) *PublishInterceptor {
	return &PublishInterceptor{
		tracer: tracer,
	}
}

// Handle implements queueapi.PublishInterceptor
func (i *PublishInterceptor) Handle(ctx context.Context, queue registry.ID, msgs []*queueapi.Message,
	next func(context.Context, registry.ID, []*queueapi.Message) error) error {
	// Get the global propagator
	propagator := otel.GetTextMapPropagator()

	// Create producer span
	spanName := "message.publish"
	if queue.String() != "" {
		spanName = queue.String() + ".publish"
	}

	ctx, span := i.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	// Set messaging attributes
	spanAttrs := []attribute.KeyValue{
		attribute.String("messaging.operation", "publish"),
		attribute.String("messaging.destination.name", queue.String()),
		attribute.Int("messaging.batch.message_count", len(msgs)),
	}

	// Add message ID only for single message (not batch)
	if len(msgs) == 1 && msgs[0].ID != "" {
		spanAttrs = append(spanAttrs, attribute.String("messaging.message.id", msgs[0].ID))
	}

	span.SetAttributes(spanAttrs...)

	// Inject trace context into each message's headers
	for _, msg := range msgs {
		if msg.Headers == nil {
			msg.Headers = attrsapi.NewBag()
		}

		carrier := &MessageHeaderCarrier{headers: msg.Headers}
		propagator.Inject(ctx, carrier)
	}

	// Continue chain
	err := next(ctx, queue, msgs)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return err
}

// extractFromDelivery extracts trace context from a queue delivery message
func extractFromDelivery(ctx context.Context, delivery *queueapi.Delivery) (context.Context, bool) {
	if delivery == nil || delivery.Message == nil || delivery.Message.Headers == nil {
		return ctx, false
	}

	// Extract trace context from message headers
	propagator := otel.GetTextMapPropagator()
	carrier := &MessageHeaderCarrier{headers: delivery.Message.Headers}
	extractedCtx := propagator.Extract(ctx, carrier)

	// Check if extraction succeeded
	spanCtx := trace.SpanContextFromContext(extractedCtx)
	return extractedCtx, spanCtx.IsValid()
}
