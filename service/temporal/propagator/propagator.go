package propagator

import (
	"context"
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

// HeaderKey is the Temporal header key for wippy context values.
// Using a distinct key to avoid conflicts with OpenTelemetry's _tracer-data.
// Format is JSON-encoded map[string]any for cross-language compatibility.
const HeaderKey = "wippy-context"

// SignalFromValueKey is the context values map key used to propagate signal sender PID.
const SignalFromValueKey = "temporal.signal.from"

var _ workflow.ContextPropagator = (*Propagator)(nil)

// Propagator implements Temporal's ContextPropagator interface
// to propagate wippy context values across workflow boundaries.
// Values are serialized as JSON for cross-language compatibility.
type Propagator struct {
	dc converter.DataConverter
}

// New creates a new context propagator.
func New(dc converter.DataConverter) *Propagator {
	return &Propagator{dc: dc}
}

// contextValuesKey is the key for storing simple context values when not using FrameContext.
type contextValuesKeyType struct{}

var contextValuesKey = contextValuesKeyType{}

// WithValues returns a context with the given values for propagation.
// Use this when you don't have a full FrameContext (e.g., in tests or simple clients).
func WithValues(ctx context.Context, values map[string]any) context.Context {
	return context.WithValue(ctx, contextValuesKey, values)
}

// GetContextValues retrieves propagated context values from a Go context.
// Returns nil if no values were propagated.
func GetContextValues(ctx context.Context) map[string]any {
	if values, ok := ctx.Value(contextValuesKey).(map[string]any); ok {
		return values
	}
	return nil
}

// Inject extracts values from Go context and writes them to Temporal headers.
// Called when starting workflows or activities from Go code.
func (p *Propagator) Inject(ctx context.Context, writer workflow.HeaderWriter) error {
	if p.dc == nil {
		return fmt.Errorf("data converter not available")
	}
	var data map[string]any

	// First, try to get values from simple context key (for tests/simple clients)
	if simpleValues, ok := ctx.Value(contextValuesKey).(map[string]any); ok && len(simpleValues) > 0 {
		data = make(map[string]any)
		for k, v := range simpleValues {
			data[k] = v
		}
	}

	// Then, try to get values from FrameContext (for full wippy infrastructure)
	values := ctxapi.GetValues(ctx)
	if values != nil && values.Len() > 0 {
		if data == nil {
			data = make(map[string]any)
		}
		values.Iterate(func(key string, val any) {
			data[key] = val
		})
	}

	data = sanitizeContextValues(data)
	if len(data) > 0 {
		payload, err := p.dc.ToPayload(data)
		if err != nil {
			return fmt.Errorf("failed to convert context to payload: %w", err)
		}

		writer.Set(HeaderKey, payload)
	}

	// Inject security context
	secPayload := ExtractSecurityPayload(ctx)
	if secPayload != nil {
		payload, err := p.dc.ToPayload(secPayload)
		if err != nil {
			return fmt.Errorf("failed to convert security to payload: %w", err)
		}

		writer.Set(SecurityHeaderKey, payload)
	}

	return nil
}

// Extract reads values from Temporal headers into Go context.
// Called when receiving workflow tasks or activity tasks.
func (p *Propagator) Extract(ctx context.Context, reader workflow.HeaderReader) (context.Context, error) {
	if p.dc == nil {
		return ctx, fmt.Errorf("data converter not available")
	}
	// Extract context values
	payload, ok := reader.Get(HeaderKey)
	if ok && payload != nil {
		var data map[string]any
		if err := p.dc.FromPayload(payload, &data); err != nil {
			return ctx, fmt.Errorf("failed to decode context values: %w", err)
		}

		if len(data) > 0 {
			// Store values in the simple context key for retrieval by GetContextValues
			ctx = context.WithValue(ctx, contextValuesKey, data)

			// Also try to set in FrameContext if available (for full wippy infrastructure)
			values, err := ctxapi.GetOrCreateValues(ctx)
			if err == nil {
				for k, v := range data {
					values.Set(k, v)
				}
			}
		}
	}

	// Extract security context
	secPayload, ok := reader.Get(SecurityHeaderKey)
	if ok && secPayload != nil {
		var sec SecurityPayload
		if err := p.dc.FromPayload(secPayload, &sec); err != nil {
			return ctx, fmt.Errorf("failed to decode security context: %w", err)
		}
		ctx = WithSecurityCtx(ctx, &sec)
	}

	return ctx, nil
}

// InjectFromWorkflow extracts values from workflow context and writes to headers.
// Called when workflow starts child workflows or activities.
func (p *Propagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	if p.dc == nil {
		return fmt.Errorf("data converter not available")
	}
	// Get values from workflow context if available
	values := sanitizeContextValues(getWorkflowValues(ctx))
	if len(values) == 0 {
		return nil
	}

	payload, err := p.dc.ToPayload(values)
	if err != nil {
		return fmt.Errorf("failed to convert context to payload: %w", err)
	}

	writer.Set(HeaderKey, payload)
	return nil
}

// ExtractToWorkflow reads values from headers into workflow context.
// Called when workflow receives headers from parent or client.
func (p *Propagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	if p.dc == nil {
		return ctx, fmt.Errorf("data converter not available")
	}
	payload, ok := reader.Get(HeaderKey)
	if !ok || payload == nil {
		return ctx, nil
	}

	var data map[string]any
	if err := p.dc.FromPayload(payload, &data); err != nil {
		return ctx, fmt.Errorf("failed to decode context values: %w", err)
	}

	if len(data) == 0 {
		return ctx, nil
	}

	// Store values in workflow context for later retrieval
	return workflow.WithValue(ctx, workflowValuesKey, data), nil
}

// workflowValuesKey is the context key for storing propagated values in workflow context.
type workflowValuesKeyType struct{}

var workflowValuesKey = workflowValuesKeyType{}

// getWorkflowValues retrieves propagated values from workflow context.
func getWorkflowValues(ctx workflow.Context) map[string]any {
	v := ctx.Value(workflowValuesKey)
	if v == nil {
		return nil
	}
	if values, ok := v.(map[string]any); ok {
		return values
	}
	return nil
}

// CreateHeader creates a Temporal header from context values.
// This is used by the workflow definition to pass context to activities and child workflows.
func CreateHeader(dc converter.DataConverter, values map[string]any) (*commonpb.Header, error) {
	if dc == nil {
		return nil, fmt.Errorf("data converter not available")
	}
	values = sanitizeContextValues(values)
	if len(values) == 0 {
		return nil, nil
	}

	payload, err := dc.ToPayload(values)
	if err != nil {
		return nil, fmt.Errorf("failed to convert context to payload: %w", err)
	}

	return &commonpb.Header{
		Fields: map[string]*commonpb.Payload{
			HeaderKey: payload,
		},
	}, nil
}

// ExtractFromHeader extracts context values directly from a Temporal header.
// This is used by the workflow definition to extract values from incoming headers.
func ExtractFromHeader(dc converter.DataConverter, header *commonpb.Header) (map[string]any, error) {
	if dc == nil {
		return nil, fmt.Errorf("data converter not available")
	}
	if header == nil || header.Fields == nil {
		return nil, nil
	}

	payload, ok := header.Fields[HeaderKey]
	if !ok || payload == nil {
		return nil, nil
	}

	var data map[string]any
	if err := dc.FromPayload(payload, &data); err != nil {
		return nil, fmt.Errorf("failed to decode context values: %w", err)
	}

	return data, nil
}

func sanitizeContextValues(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]any, len(values))
	for k, v := range values {
		if normalized, ok := normalizeContextValue(v); ok {
			out[k] = normalized
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeContextValue(v any) (any, bool) {
	switch val := v.(type) {
	case string, int, int64, float64, bool:
		return val, true
	case map[string]any:
		nested := make(map[string]any, len(val))
		for k, inner := range val {
			norm, ok := normalizeContextValue(inner)
			if !ok {
				return nil, false
			}
			nested[k] = norm
		}
		return nested, true
	case []any:
		nested := make([]any, 0, len(val))
		for _, inner := range val {
			norm, ok := normalizeContextValue(inner)
			if !ok {
				return nil, false
			}
			nested = append(nested, norm)
		}
		return nested, true
	default:
		return nil, false
	}
}
