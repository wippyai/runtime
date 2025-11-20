package otel

import (
	stdcontext "context"
	"net/http"

	ctxapi "github.com/wippyai/runtime/api/context"
	apiinterceptor "github.com/wippyai/runtime/api/function"
	apiprocess "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	httpapi "github.com/wippyai/runtime/api/service/http"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Service implements the OTEL service interface
type Service struct {
	cfg    otelapi.Config
	logger *zap.Logger
	tracer trace.Tracer
}

// NewService creates a new OTEL service
func NewService(cfg otelapi.Config, logger *zap.Logger, provider trace.TracerProvider) *Service {
	tracer := provider.Tracer("wippy-runtime")

	return &Service{
		cfg:    cfg,
		logger: logger,
		tracer: tracer,
	}
}

// HTTPMiddleware returns HTTP middleware for trace context propagation
func (s *Service) HTTPMiddleware() func(http.Handler) http.Handler {
	if !s.cfg.HTTP.Enabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if s.cfg.HTTP.ExtractHeaders {
				ctx = propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))
			}

			spanName := r.Method + " " + r.URL.Path

			ctx, span := s.tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.host", r.Host),
					attribute.String("http.target", r.URL.Path),
				),
			)
			defer span.End()

			_ = otelapi.SetSpan(ctx, span)

			if s.cfg.HTTP.InjectHeaders {
				propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))
			}

			r = r.WithContext(ctx)

			// Check for RouteInfo before calling next handler (safe read for post-middleware)
			if routeInfo, ok := httpapi.GetRouteInfo(ctx); ok {
				if routeInfo != nil && routeInfo.Func.Name != "" {
					span.SetName(r.Method + " " + routeInfo.Func.String())
					span.SetAttributes(attribute.String("http.route", routeInfo.Func.String()))
				} else if routeInfo != nil && routeInfo.Endpoint.Name != "" {
					span.SetName(r.Method + " " + routeInfo.Endpoint.String())
					span.SetAttributes(attribute.String("http.route", routeInfo.Endpoint.String()))
				}
			}

			next.ServeHTTP(w, r)

			span.SetAttributes(attribute.Int("http.status_code", 200))
		})
	}
}

// ProcessMutator returns a StartMutator for process lifecycle tracing
func (s *Service) ProcessMutator() apiprocess.StartMutator {
	if !s.cfg.Process.Enabled {
		return func(ctx stdcontext.Context, _ *apiprocess.Start) (stdcontext.Context, error) {
			return ctx, nil
		}
	}

	return func(ctx stdcontext.Context, start *apiprocess.Start) (stdcontext.Context, error) {
		if !s.cfg.Process.TraceLifecycle {
			return ctx, nil
		}

		// Extract parent SpanContext (not the span itself)
		var parentSpanContext trace.SpanContext
		if parentSpan, hasParent := otelapi.GetSpan(ctx); hasParent {
			parentSpanContext = parentSpan.SpanContext()
		} else {
			parentSpanContext = trace.SpanContextFromContext(ctx)
		}

		// Create process wrapper span and close it immediately (Temporal pattern)
		spanName := "process:" + start.Source.String()
		var processSpan trace.Span

		if parentSpanContext.IsValid() {
			ctxWithParent := trace.ContextWithRemoteSpanContext(ctx, parentSpanContext)
			_, processSpan = s.tracer.Start(ctxWithParent, spanName,
				trace.WithSpanKind(trace.SpanKindInternal))
		} else {
			_, processSpan = s.tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer))
		}

		processSpan.SetAttributes(
			attribute.String("process.source", start.Source.String()),
		)
		processSpan.End()

		// Store process's own SpanContext for child processes/functions to use
		start.Context = append(start.Context, ctxapi.Pair{
			Key:   otelapi.GetRemoteSpanContextKey(),
			Value: processSpan.SpanContext(),
		})

		// Add OnStart hook for process start tracing
		if hook := s.ProcessStartHook(); hook != nil {
			start.OnStart = append(start.OnStart, hook)
		}

		// Add OnComplete hook for process termination tracing
		if hook := s.ProcessCompleteHook(); hook != nil {
			start.OnComplete = append(start.OnComplete, hook)
		}

		return ctx, nil
	}
}

// ProcessStartHook returns an OnStart hook for process start tracing
func (s *Service) ProcessStartHook() apiprocess.OnStart {
	if !s.cfg.Process.Enabled {
		return nil
	}

	return func(ctx stdcontext.Context, pid relay.PID, _ apiprocess.Process) {
		if !s.cfg.Process.TraceLifecycle {
			return
		}

		// Get the parent remote SpanContext from process context
		parentSpanCtx, hasParent := otelapi.GetRemoteSpanContext(ctx)
		if !hasParent || !parentSpanCtx.IsValid() {
			return
		}

		sourceID, hasSource := runtime.GetFrameID(ctx)
		processSpanName := "process.start"
		if hasSource {
			processSpanName = sourceID.String()
		}

		ctxWithParent := trace.ContextWithRemoteSpanContext(ctx, parentSpanCtx)
		_, processSpan := s.tracer.Start(ctxWithParent, processSpanName,
			trace.WithSpanKind(trace.SpanKindInternal))

		processSpan.SetAttributes(
			attribute.String("process.pid", pid.String()),
		)
		if hasSource {
			processSpan.SetAttributes(attribute.String("process.source", sourceID.String()))
		}
		processSpan.End()

		// Store this process's SpanContext for child functions to use
		processSpanCtx := processSpan.SpanContext()
		fc := ctxapi.FrameFromContext(ctx)
		if fc != nil {
			_ = fc.Set(otelapi.GetRemoteSpanContextKey(), processSpanCtx)
		}

		startEventName := "process.started"
		if hasSource {
			startEventName = sourceID.String() + ".started"
		}

		ctxWithProcess := trace.ContextWithRemoteSpanContext(ctx, processSpanCtx)
		_, startSpan := s.tracer.Start(ctxWithProcess, startEventName,
			trace.WithSpanKind(trace.SpanKindInternal))

		startSpan.SetAttributes(
			attribute.String("process.pid", pid.String()),
			attribute.String("lifecycle.event", "started"),
		)
		if hasSource {
			startSpan.SetAttributes(attribute.String("process.source", sourceID.String()))
		}
		startSpan.End()
	}
}

// ProcessCompleteHook returns an OnComplete hook for process termination tracing
func (s *Service) ProcessCompleteHook() apiprocess.OnComplete {
	if !s.cfg.Process.Enabled {
		return nil
	}

	return func(ctx stdcontext.Context, pid relay.PID, result *runtime.Result) {
		if !s.cfg.Process.TraceLifecycle {
			return
		}

		// Try to get the remote SpanContext from process context
		remoteSpanCtx, hasRemote := otelapi.GetRemoteSpanContext(ctx)
		if !hasRemote || !remoteSpanCtx.IsValid() {
			return
		}

		sourceID, hasSource := runtime.GetFrameID(ctx)
		spanName := "process.terminated"
		if hasSource {
			spanName = sourceID.String() + ".terminated"
		}

		ctxWithRemote := trace.ContextWithRemoteSpanContext(ctx, remoteSpanCtx)
		_, span := s.tracer.Start(ctxWithRemote, spanName,
			trace.WithSpanKind(trace.SpanKindInternal))

		attrs := []attribute.KeyValue{
			attribute.String("process.pid", pid.String()),
			attribute.String("lifecycle.event", "terminated"),
		}

		if hasSource {
			attrs = append(attrs, attribute.String("process.source", sourceID.String()))
		}

		if result != nil && result.Error != nil {
			span.RecordError(result.Error)
			span.SetStatus(codes.Error, result.Error.Error())
			attrs = append(attrs, attribute.String("termination.reason", "error"))
		} else {
			span.SetStatus(codes.Ok, "")
			attrs = append(attrs, attribute.String("termination.reason", "completed"))
		}

		span.SetAttributes(attrs...)
		span.End()
	}
}

// Interceptor returns the function call interceptor
func (s *Service) Interceptor() apiinterceptor.Interceptor {
	if !s.cfg.Interceptor.Enabled {
		return nil
	}

	return &interceptor{
		tracer: s.tracer,
		logger: s.logger,
	}
}

// interceptor implements the OTEL interceptor
type interceptor struct {
	tracer trace.Tracer
	logger *zap.Logger
}

// Handle implements the interceptor interface
func (i *interceptor) Handle(ctx stdcontext.Context, task runtime.Task, next func(stdcontext.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	spanName := task.ID.String()
	if spanName == "" || spanName == ":" {
		spanName = "function_execution"
	}

	var span trace.Span

	// Priority 1: Use active parent span (from HTTP or parent function call)
	if parentSpan, hasParent := otelapi.GetSpan(ctx); hasParent {
		ctx, span = i.tracer.Start(trace.ContextWithSpan(ctx, parentSpan), spanName,
			trace.WithSpanKind(trace.SpanKindInternal))
	} else if remoteSpanCtx, hasRemote := otelapi.GetRemoteSpanContext(ctx); hasRemote && remoteSpanCtx.IsValid() {
		// Priority 2: Check for remote SpanContext (from process)
		ctxWithRemote := trace.ContextWithRemoteSpanContext(ctx, remoteSpanCtx)
		ctxWithRemote, span = i.tracer.Start(ctxWithRemote, spanName,
			trace.WithSpanKind(trace.SpanKindInternal))
		ctx = ctxWithRemote
	} else {
		// Priority 3: No parent context - create root span
		ctx, span = i.tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer))
	}
	defer span.End()

	attrs := make([]attribute.KeyValue, 0, 8)

	if pid, ok := runtime.GetFramePID(ctx); ok {
		attrs = append(attrs, attribute.String("process.pid", pid.String()))
	}

	if fid, ok := runtime.GetFrameID(ctx); ok {
		attrs = append(attrs, attribute.String("frame.id", fid.String()))
	}

	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		fc.Iterate(func(key any, value any) {
			if k, ok := key.(*ctxapi.Key); ok && k != nil {
				switch v := value.(type) {
				case string:
					attrs = append(attrs, attribute.String("ctx."+k.Name, v))
				case int:
					attrs = append(attrs, attribute.Int("ctx."+k.Name, v))
				case int64:
					attrs = append(attrs, attribute.Int64("ctx."+k.Name, v))
				case bool:
					attrs = append(attrs, attribute.Bool("ctx."+k.Name, v))
				}
			}
		})
	}

	if bag, ok := task.Options.(runtime.Bag); ok && bag != nil {
		for key, value := range bag {
			switch v := value.(type) {
			case string:
				attrs = append(attrs, attribute.String("opt."+key, v))
			case int:
				attrs = append(attrs, attribute.Int("opt."+key, v))
			case int64:
				attrs = append(attrs, attribute.Int64("opt."+key, v))
			case float64:
				attrs = append(attrs, attribute.Float64("opt."+key, v))
			case bool:
				attrs = append(attrs, attribute.Bool("opt."+key, v))
			}
		}
	}

	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	task.Context = append(task.Context, ctxapi.Pair{Key: otelapi.GetSpanKey(), Value: span})

	result, err := next(ctx, task)
	switch {
	case err != nil:
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	case result != nil && result.Error != nil:
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
	default:
		span.SetStatus(codes.Ok, "")
	}

	return result, err
}
