// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

func TestGetOrigins(t *testing.T) {
	assert.Equal(t, "a", getOrigins(map[string]string{
		OptionAllowedOrigins: "a",
		sharedAllowOrigins:   "b",
		legacyAllowedOrigins: "c",
	}))
	assert.Equal(t, "b", getOrigins(map[string]string{
		sharedAllowOrigins:   "b",
		legacyAllowedOrigins: "c",
	}))
	assert.Equal(t, "c", getOrigins(map[string]string{
		legacyAllowedOrigins: "c",
	}))
	assert.Equal(t, "", getOrigins(map[string]string{}))
}

func TestOriginAllowed(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/sse", nil)
	req.Header.Set("Origin", "https://example.com")
	assert.True(t, originAllowed(req, nil), "same-origin should pass default policy")

	req.Header.Set("Origin", "https://evil.com")
	assert.False(t, originAllowed(req, nil), "cross-origin should fail default policy")

	assert.True(t, originAllowed(req, []string{"*"}))
	assert.True(t, originAllowed(req, []string{"https://evil.com"}))
	assert.True(t, originAllowed(req, []string{"https://*.com"}))
	assert.False(t, originAllowed(req, []string{"https://allowed.com"}))
}

func TestResponseWrapper(t *testing.T) {
	w := httptest.NewRecorder()
	rw := newResponseWrapper(w)

	rw.Header().Set("X-Test", "1")
	assert.Equal(t, "1", rw.Header().Get("X-Test"))

	_, err := rw.Write([]byte("abc"))
	require.NoError(t, err)
	assert.True(t, rw.wroteHeader)
	assert.True(t, rw.wroteBody)
	assert.Equal(t, "abc", w.Body.String())

	rw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rw.statusCode)
}

func TestMiddlewareNoRelayHeader(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})
	called := false
	mgr.newSession = func(
		_ context.Context,
		_ RelayCommand,
		_ registry.ID,
		_ relay.AttachableReceiver,
		_ relay.Node,
		_ topology.Topology,
		_ payload.Transcoder,
		_ process.PIDGenerator,
		_ *zap.Logger,
	) (sessionRunner, error) {
		called = true
		return &stubSession{}, nil
	}

	handler := mgr.middlewareHandler(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}), nil)
	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.False(t, called)
}

func TestMiddlewareStartsSession(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})
	stub := &stubSession{}
	called := false
	mgr.newSession = func(
		_ context.Context,
		cfg RelayCommand,
		_ registry.ID,
		_ relay.AttachableReceiver,
		_ relay.Node,
		_ topology.Topology,
		_ payload.Transcoder,
		_ process.PIDGenerator,
		_ *zap.Logger,
	) (sessionRunner, error) {
		called = true
		assert.Equal(t, "llm.delta", cfg.MessageTopic)
		return stub, nil
	}

	handler := mgr.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(RelayCommand{MessageTopic: "llm.delta"})
		w.Header().Set(RelayHeader, string(data))
	}), nil)

	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.True(t, stub.serveCalled)
	assert.NotEmpty(t, stub.closeReasons)
}

func TestMiddlewareRejectsInvalidConfig(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})

	handler := mgr.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(RelayHeader, "{bad-json")
	}), nil)

	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid relay configuration")
}

func TestMiddlewareRejectsOrigin(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})

	handler := mgr.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(RelayCommand{})
		w.Header().Set(RelayHeader, string(data))
	}), nil)

	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	req.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestMiddlewareSkipsWhenBodyAlreadyWritten(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})
	called := false
	mgr.newSession = func(
		_ context.Context,
		_ RelayCommand,
		_ registry.ID,
		_ relay.AttachableReceiver,
		_ relay.Node,
		_ topology.Topology,
		_ payload.Transcoder,
		_ process.PIDGenerator,
		_ *zap.Logger,
	) (sessionRunner, error) {
		called = true
		return &stubSession{}, nil
	}

	handler := mgr.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(RelayCommand{})
		w.Header().Set(RelayHeader, string(data))
		_, _ = w.Write([]byte("already written"))
	}), nil)

	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.False(t, called)
}

func TestMiddlewareSkipsWhenNon200HeaderAlreadyWritten(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})
	called := false
	mgr.newSession = func(
		_ context.Context,
		_ RelayCommand,
		_ registry.ID,
		_ relay.AttachableReceiver,
		_ relay.Node,
		_ topology.Topology,
		_ payload.Transcoder,
		_ process.PIDGenerator,
		_ *zap.Logger,
	) (sessionRunner, error) {
		called = true
		return &stubSession{}, nil
	}

	handler := mgr.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(RelayCommand{})
		w.Header().Set(RelayHeader, string(data))
		w.WriteHeader(http.StatusAccepted)
	}), nil)

	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.False(t, called)
}

func TestMiddlewareSessionCreateErrorStatus(t *testing.T) {
	mgr := NewSSERelay(context.Background(), zap.NewNop(), &testPIDGen{})
	mgr.newSession = func(
		_ context.Context,
		_ RelayCommand,
		_ registry.ID,
		_ relay.AttachableReceiver,
		_ relay.Node,
		_ topology.Topology,
		_ payload.Transcoder,
		_ process.PIDGenerator,
		_ *zap.Logger,
	) (sessionRunner, error) {
		return nil, ErrHostRequired
	}

	handler := mgr.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(RelayCommand{})
		w.Header().Set(RelayHeader, string(data))
	}), nil)

	req := withFrame(t, newTestRequest(t, "http://example.com/sse"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHTTPStatusFromError(t *testing.T) {
	tests := []struct {
		err  error
		name string
		want int
	}{
		{
			name: "invalid",
			err:  apierror.New(apierror.Invalid, "bad"),
			want: http.StatusBadRequest,
		},
		{
			name: "permission denied",
			err:  apierror.New(apierror.PermissionDenied, "forbidden"),
			want: http.StatusForbidden,
		},
		{
			name: "not found",
			err:  apierror.New(apierror.NotFound, "missing"),
			want: http.StatusNotFound,
		},
		{
			name: "conflict",
			err:  apierror.New(apierror.Conflict, "conflict"),
			want: http.StatusConflict,
		},
		{
			name: "timeout",
			err:  apierror.New(apierror.Timeout, "timeout"),
			want: http.StatusRequestTimeout,
		},
		{
			name: "rate limited",
			err:  apierror.New(apierror.RateLimited, "slow down"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "unavailable",
			err:  apierror.New(apierror.Unavailable, "down"),
			want: http.StatusServiceUnavailable,
		},
		{
			name: "non api error",
			err:  errors.New("boom"),
			want: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, httpStatusFromError(tc.err))
		})
	}
}

func newTestRequest(_ *testing.T, url string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
}

func withFrame(t *testing.T, req *http.Request) *http.Request {
	ctx, fc := contextapi.OpenFrameContext(req.Context())
	t.Cleanup(func() { contextapi.ReleaseFrameContext(fc) })
	require.NoError(t, fc.Set(httpapi.ServerKey(), &testAttachableHost{}))
	require.NoError(t, fc.Set(httpapi.ServerIDKey(), "app:test_server"))
	return req.WithContext(ctx)
}

type stubSession struct {
	serveErr     error
	closeReasons []string
	serveCalled  bool
}

func (s *stubSession) Serve(_ context.Context, _ http.ResponseWriter) error {
	s.serveCalled = true
	return s.serveErr
}

func (s *stubSession) Close(reason string) {
	s.closeReasons = append(s.closeReasons, reason)
}

type testAttachableHost struct{}

func (h *testAttachableHost) Send(_ *relay.Package) error {
	return nil
}

func (h *testAttachableHost) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}

func (h *testAttachableHost) Detach(_ pid.PID) {}
