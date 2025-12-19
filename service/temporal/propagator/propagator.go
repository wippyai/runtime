package propagator

import (
	"context"
	"encoding/json"
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

var _ workflow.ContextPropagator = (*Propagator)(nil)

// Propagator implements Temporal's ContextPropagator interface
// to propagate wippy context values across workflow boundaries.
// Values are serialized as JSON for cross-language compatibility.
type Propagator struct{}

// New creates a new context propagator.
func New() *Propagator {
	return &Propagator{}
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
			// Only propagate JSON-serializable types
			switch val.(type) {
			case string, int, int64, float64, bool, map[string]any, []any:
				data[key] = val
			}
		})
	}

	if len(data) > 0 {
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to serialize context values: %w", err)
		}

		payload, err := converter.GetDefaultDataConverter().ToPayload(jsonBytes)
		if err != nil {
			return fmt.Errorf("failed to convert context to payload: %w", err)
		}

		writer.Set(HeaderKey, payload)
	}

	// Inject security context
	secPayload := ExtractSecurityPayload(ctx)
	if secPayload != nil {
		jsonBytes, err := json.Marshal(secPayload)
		if err != nil {
			return fmt.Errorf("failed to serialize security context: %w", err)
		}

		payload, err := converter.GetDefaultDataConverter().ToPayload(jsonBytes)
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
	// Extract context values
	payload, ok := reader.Get(HeaderKey)
	if ok && payload != nil {
		var jsonBytes []byte
		if err := converter.GetDefaultDataConverter().FromPayload(payload, &jsonBytes); err != nil {
			return ctx, fmt.Errorf("failed to decode context payload: %w", err)
		}

		var data map[string]any
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return ctx, fmt.Errorf("failed to unmarshal context values: %w", err)
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
		var jsonBytes []byte
		if err := converter.GetDefaultDataConverter().FromPayload(secPayload, &jsonBytes); err != nil {
			return ctx, fmt.Errorf("failed to decode security payload: %w", err)
		}
		var sec SecurityPayload
		if err := json.Unmarshal(jsonBytes, &sec); err != nil {
			return ctx, fmt.Errorf("failed to unmarshal security context: %w", err)
		}
		ctx = WithSecurityCtx(ctx, &sec)
	}

	return ctx, nil
}

// InjectFromWorkflow extracts values from workflow context and writes to headers.
// Called when workflow starts child workflows or activities.
func (p *Propagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	// Get values from workflow context if available
	values := getWorkflowValues(ctx)
	if len(values) == 0 {
		return nil
	}

	jsonBytes, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to serialize context values: %w", err)
	}

	payload, err := converter.GetDefaultDataConverter().ToPayload(jsonBytes)
	if err != nil {
		return fmt.Errorf("failed to convert context to payload: %w", err)
	}

	writer.Set(HeaderKey, payload)
	return nil
}

// ExtractToWorkflow reads values from headers into workflow context.
// Called when workflow receives headers from parent or client.
func (p *Propagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	payload, ok := reader.Get(HeaderKey)
	if !ok || payload == nil {
		return ctx, nil
	}

	var jsonBytes []byte
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &jsonBytes); err != nil {
		return ctx, fmt.Errorf("failed to decode context payload: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return ctx, fmt.Errorf("failed to unmarshal context values: %w", err)
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
func CreateHeader(values map[string]any) (*commonpb.Header, error) {
	if len(values) == 0 {
		return nil, nil
	}

	jsonBytes, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize context values: %w", err)
	}

	payload, err := converter.GetDefaultDataConverter().ToPayload(jsonBytes)
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
func ExtractFromHeader(header *commonpb.Header) (map[string]any, error) {
	if header == nil || header.Fields == nil {
		return nil, nil
	}

	payload, ok := header.Fields[HeaderKey]
	if !ok || payload == nil {
		return nil, nil
	}

	var jsonBytes []byte
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &jsonBytes); err != nil {
		return nil, fmt.Errorf("failed to decode context payload: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal context values: %w", err)
	}

	return data, nil
}
