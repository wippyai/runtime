// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	relaysys "github.com/wippyai/runtime/system/relay"

	contextapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/logs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	config "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/runtime/security"
)

const (
	// BootTimeout is the maximum time to wait for the server to start
	BootTimeout = 30 * time.Second

	// CheckInterval is the interval between server availability checks during startup
	CheckInterval = 100 * time.Millisecond

	// StatusBuffer is the size of the status channel buffer
	StatusBuffer = 10
)

// ServerService combines HTTP server and router functionality
type ServerService struct {
	ctx           context.Context
	handlerFunc   http.Handler
	middlewareFac MiddlewareAPI
	host          relay.AttachableReceiver
	statusChan    chan any
	server        *http.Server
	mountPaths    map[registry.ID]string
	mountHandlers map[registry.ID]http.Handler
	routeMgr      *RouteManager
	config        *config.ServerConfig
	id            registry.ID
	mu            sync.RWMutex
	shutdownOnce  sync.Once
	started       atomic.Bool
}

// NewServerService creates a new ServerService instance
func NewServerService(id registry.ID, cfg *config.ServerConfig, middleware MiddlewareAPI) (*ServerService, error) {
	routeManager, err := NewRouteManager()
	if err != nil {
		return nil, err
	}

	return &ServerService{
		id:            id,
		config:        cfg,
		routeMgr:      routeManager,
		statusChan:    make(chan any, StatusBuffer),
		mountPaths:    make(map[registry.ID]string),
		mountHandlers: make(map[registry.ID]http.Handler),
		middlewareFac: middleware,
	}, nil
}

// SetHandlerFunc sets the server-level handler function
func (s *ServerService) SetHandlerFunc(handler http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerFunc = handler
}

// UpdateConfig updates the server configuration
// Returns an error if trying to change the address while the server is running
func (s *ServerService) UpdateConfig(cfg *config.ServerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if address changes while server is running
	if s.started.Load() {
		if s.config.Addr != cfg.Addr {
			return ErrServerAddressChangeWhileRunning
		}
	}

	// Always update the config
	s.config = cfg
	return nil
}

// UpsertRouter adds a new router or updates an existing one with the provided configuration
func (s *ServerService) UpsertRouter(id registry.ID, cfg *config.RouterConfig) error {
	// Convert middleware config to actual middleware functions
	middlewares := make([]func(http.Handler) http.Handler, 0, len(cfg.Middleware))
	if s.middlewareFac != nil {
		for _, mw := range cfg.Middleware {
			m, err := s.middlewareFac.CreateMiddleware(mw, cfg.Options)
			if err != nil {
				return NewMiddlewareCreateError(mw, err)
			}

			middlewares = append(middlewares, m)
		}
	}

	// Convert post-match middleware config to actual middleware functions
	postMiddlewares := make([]func(http.Handler) http.Handler, 0, len(cfg.PostMiddleware))
	if s.middlewareFac != nil {
		for _, mw := range cfg.PostMiddleware {
			m, err := s.middlewareFac.CreateMiddleware(mw, cfg.PostOptions)
			if err != nil {
				return NewPostMiddlewareCreateError(mw, err)
			}

			postMiddlewares = append(postMiddlewares, m)
		}
	}

	return s.routeMgr.AddRouter(id, cfg.Prefix, middlewares, postMiddlewares)
}

// DeleteRouter removes a router by Source
func (s *ServerService) DeleteRouter(id registry.ID) error {
	return s.routeMgr.RemoveRouter(id)
}

// UpsertEndpoint adds or updates an endpoint in the specified router
func (s *ServerService) UpsertEndpoint(routerID, id registry.ID, path string, method string, handler http.Handler) error {
	return s.routeMgr.AddRoute(routerID, id, method, path, id, handler)
}

// RemoveEndpoint removes an endpoint from the specified router
func (s *ServerService) RemoveEndpoint(routerID, id registry.ID) error {
	return s.routeMgr.RemoveRoute(routerID, id)
}

// Mount adds a handler at the specified path and tracks it by Source
func (s *ServerService) Mount(id registry.ID, path string, handler http.Handler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldPath, exists := s.mountPaths[id]; exists {
		if oldPath == path {
			if err := s.routeMgr.ReplaceMount(path, handler); err != nil {
				return err
			}
			s.mountHandlers[id] = handler
			return nil
		}

		oldHandler := s.mountHandlers[id]
		if err := s.routeMgr.Unmount(oldPath); err != nil {
			return err
		}
		if err := s.routeMgr.Mount(path, handler); err != nil {
			if oldHandler != nil {
				_ = s.routeMgr.Mount(oldPath, oldHandler)
			}
			return err
		}

		s.mountPaths[id] = path
		s.mountHandlers[id] = handler
		return nil
	}

	if err := s.routeMgr.Mount(path, handler); err != nil {
		return err
	}

	// Store path mapping for later unmount
	s.mountPaths[id] = path
	s.mountHandlers[id] = handler
	return nil
}

// Remove unmounts a handler by Source
func (s *ServerService) Remove(id registry.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, exists := s.mountPaths[id]
	if !exists {
		return NewMountNotFoundError(id.String())
	}

	if err := s.routeMgr.Unmount(path); err != nil {
		return err
	}

	// Clean up the mapping
	delete(s.mountPaths, id)
	delete(s.mountHandlers, id)
	return nil
}

// Rebuild rebuilds the entire router with the current configuration
func (s *ServerService) Rebuild(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If handler function is set, don't rebuild router
	if s.handlerFunc != nil {
		return nil
	}

	err := s.routeMgr.Build()
	if err != nil {
		return err
	}

	return nil
}

// Start implements the supervisor.Service interface to start the HTTP server
func (s *ServerService) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()

	// Initialize mailbox with config
	bufferSize := s.config.Host.BufferSize
	if bufferSize <= 0 {
		bufferSize = 1024 // Default buffer size
	}

	workerCount := s.config.Host.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.NumCPU() // Default to number of CPUs
	}

	// Create the mailbox
	s.host = relaysys.NewMailbox(ctx,
		relaysys.WithBufferSize(bufferSize),
		relaysys.WithWorkerCount(workerCount),
		relaysys.WithLogger(logs.GetLogger(ctx)),
	)

	s.ctx = ctx

	// Use handler function if set, otherwise use route manager
	var baseHandler http.Handler
	if s.handlerFunc != nil {
		baseHandler = s.handlerFunc
	} else {
		baseHandler = s.routeMgr
	}

	// Wrap handler with per-request FrameContext creation
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always fork a dedicated request frame from the inbound context.
		ctx, fc := contextapi.ForkFrameContext(r.Context())
		defer contextapi.ReleaseFrameContext(fc)

		// Set all HTTP-specific metadata in FrameContext in one place
		_ = config.SetServerID(ctx, s.id.String())
		_ = config.SetServerHost(ctx, s.config.Addr)
		_ = fc.Set(config.ServerKey(), s)

		baseHandler.ServeHTTP(w, r.WithContext(ctx))
	})

	srv := &http.Server{
		Addr:         s.config.Addr,
		Handler:      handler,
		ReadTimeout:  s.config.Timeouts.ReadTimeout,
		WriteTimeout: s.config.Timeouts.WriteTimeout,
		IdleTimeout:  s.config.Timeouts.IdleTimeout,
		BaseContext: func(_ net.Listener) context.Context {
			// Return app-level context only
			return s.ctx
		},
	}

	ln, probe, err := s.buildListener(ctx)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.server = srv
	s.started.Store(true)

	s.mu.Unlock()

	// Launch server
	go func() {
		// Use the captured server instance to avoid racing on s.server field.
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case s.statusChan <- NewServerError(err):
			default:
			}
		}

		s.started.Store(false)
	}()

	if err := s.ensureRunning(ctx, probe); err != nil {
		s.started.Store(false)
		return nil, NewStartupCheckError(err)
	}

	// Handle shutdown via context (only once)
	s.shutdownOnce.Do(func() {
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := s.Stop(shutdownCtx); err != nil {
				select {
				case s.statusChan <- NewShutdownError(err):
				default:
				}
			}
		}()
	})

	select {
	case s.statusChan <- fmt.Sprintf("service listening on %s", s.config.Addr):
	default:
	}

	return s.statusChan, nil
}

// Stop implements the supervisor.Service interface to stop the HTTP server
func (s *ServerService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Gracefully shutdown the server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return NewGracefulShutdownError(err)
		}
		s.server = nil
	}

	// Host will be cleaned up via context cancellation
	s.host = nil
	s.started.Store(false)

	return nil
}

// probeFunc dials the server's bind fabric — clearnet for addr-only
// services, and the overlay driver when cfg.Network is set.
type probeFunc func(ctx context.Context, addr string) (net.Conn, error)

// ensureRunning verifies that the server is listening by dialing itself on
// the same fabric it bound on.
func (s *ServerService) ensureRunning(ctx context.Context, probe probeFunc) error {
	timeout := time.After(BootTimeout)
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return NewStartupTimeoutError(BootTimeout.String())
		case <-ctx.Done():
			return NewStartupCanceledError(ctx.Err())
		case <-ticker.C:
			dialCtx, cancel := context.WithTimeout(ctx, time.Second)
			conn, err := probe(dialCtx, s.config.Addr)
			cancel()
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
	}
}

// buildListener resolves cfg.Network (if any) against the network registry,
// enforces the network.bind permission, and returns a ready net.Listener
// along with a probe dialer for ensureRunning. TLS is layered per cfg.TLS:
//   - TLSModeOff: plain listener on driver or clearnet
//   - TLSModeAuto: driver must implement netapi.TLSListener (tsnet does)
//   - TLSModeManual: loads cert/key, wraps listener in tls.NewListener
func (s *ServerService) buildListener(ctx context.Context) (net.Listener, probeFunc, error) {
	if s.config.Network.Name != "" {
		reg := netapi.GetNetworkRegistry(ctx)
		if reg == nil {
			return nil, nil, ErrNetworkRegistryNotAvailable
		}
		svc, err := reg.GetNetwork(s.config.Network)
		if err != nil {
			return nil, nil, NewNetworkResolveError(s.config.Network.String(), err)
		}

		if !security.IsAllowed(ctx, "network.bind", s.config.Network.String(), nil) {
			return nil, nil, NewNetworkBindDeniedError(s.config.Network.String())
		}

		ln, err := s.overlayListen(ctx, svc)
		if err != nil {
			return nil, nil, err
		}

		probe := func(ctx context.Context, addr string) (net.Conn, error) {
			return svc.DialContext(ctx, "tcp", addr)
		}
		return ln, probe, nil
	}

	ln, err := s.clearnetListen(ctx)
	if err != nil {
		return nil, nil, err
	}
	probe := func(ctx context.Context, addr string) (net.Conn, error) {
		d := &net.Dialer{}
		return d.DialContext(ctx, "tcp", addr)
	}
	return ln, probe, nil
}

// overlayListen binds through a network driver, layering TLS per cfg.
func (s *ServerService) overlayListen(ctx context.Context, svc netapi.Service) (net.Listener, error) {
	switch s.config.TLS.Mode {
	case config.TLSModeAuto:
		tlsSvc, ok := svc.(netapi.TLSListener)
		if !ok {
			return nil, NewNetworkAutoTLSUnsupportedError(s.config.Network.String())
		}
		ln, err := tlsSvc.ListenTLS(ctx, "tcp", s.config.Addr)
		if err != nil {
			return nil, NewNetworkListenError(s.config.Network.String(), err)
		}
		return ln, nil
	case config.TLSModeManual:
		raw, err := svc.Listen(ctx, "tcp", s.config.Addr)
		if err != nil {
			return nil, NewNetworkListenError(s.config.Network.String(), err)
		}
		cfg, err := loadServerTLSConfig(ctx, s.config.TLS)
		if err != nil {
			raw.Close()
			return nil, err
		}
		return tls.NewListener(raw, cfg), nil
	default:
		ln, err := svc.Listen(ctx, "tcp", s.config.Addr)
		if err != nil {
			return nil, NewNetworkListenError(s.config.Network.String(), err)
		}
		return ln, nil
	}
}

// clearnetListen binds on the host fabric, layering manual TLS if requested.
// TLS auto is rejected without an overlay driver because plain http.Server
// has no ACME integration.
func (s *ServerService) clearnetListen(ctx context.Context) (net.Listener, error) {
	if s.config.TLS.Mode == config.TLSModeAuto {
		return nil, ErrClearnetAutoTLSUnsupported
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.config.Addr)
	if err != nil {
		return nil, err
	}
	if s.config.TLS.Mode == config.TLSModeManual {
		cfg, err := loadServerTLSConfig(ctx, s.config.TLS)
		if err != nil {
			ln.Close()
			return nil, err
		}
		return tls.NewListener(ln, cfg), nil
	}
	return ln, nil
}

// loadServerTLSConfig resolves cert/key material (inline PEM or env-backed)
// and assembles a tls.Config. TLS 1.2 is the floor — 1.0/1.1 have been
// deprecated since 2020 and are not user-selectable. Optional mTLS layers
// a ClientCAs pool + ClientAuth policy on top.
func loadServerTLSConfig(ctx context.Context, cfg config.ServerTLSConfig) (*tls.Config, error) {
	certPEM, keyPEM, err := resolveCertKeyPEM(ctx, cfg)
	if err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, NewTLSLoadError(err)
	}

	out := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.ClientAuth == config.ClientAuthNone && cfg.ClientCA == "" && cfg.ClientCAEnv == "" {
		return out, nil
	}

	out.ClientAuth = mapClientAuthType(cfg.ClientAuth)

	caPEM, err := resolveClientCAPEM(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, NewTLSCAParseError()
		}
		out.ClientCAs = pool
	}
	return out, nil
}

// resolveCertKeyPEM returns the cert+key PEM bytes for Manual mode. The
// config validator has already enforced the (inline XOR env) invariant.
func resolveCertKeyPEM(ctx context.Context, cfg config.ServerTLSConfig) ([]byte, []byte, error) {
	if cfg.Cert != "" {
		return []byte(cfg.Cert), []byte(cfg.Key), nil
	}
	certPEM, err := lookupEnv(ctx, cfg.CertEnv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := lookupEnv(ctx, cfg.KeyEnv)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

// resolveClientCAPEM returns the optional client-CA PEM bundle (empty if
// the user opted into a non-verifying auth mode without providing a pool).
func resolveClientCAPEM(ctx context.Context, cfg config.ServerTLSConfig) ([]byte, error) {
	if cfg.ClientCA != "" {
		return []byte(cfg.ClientCA), nil
	}
	if cfg.ClientCAEnv != "" {
		return lookupEnv(ctx, cfg.ClientCAEnv)
	}
	return nil, nil
}

// lookupEnv reads a named variable from the Wippy env registry (secure
// store). Absence of the registry or variable is a hard error — callers
// only reach this path when the config explicitly asked for env-backed
// material.
func lookupEnv(ctx context.Context, name string) ([]byte, error) {
	reg := envapi.GetRegistry(ctx)
	if reg == nil {
		return nil, ErrTLSEnvRegistryUnavailable
	}
	val, err := reg.Get(ctx, name)
	if err != nil {
		return nil, NewTLSEnvResolveError(name, err)
	}
	return []byte(val), nil
}

func mapClientAuthType(a config.ClientAuthType) tls.ClientAuthType {
	switch a {
	case config.ClientAuthRequest:
		return tls.RequestClientCert
	case config.ClientAuthRequireAny:
		return tls.RequireAnyClientCert
	case config.ClientAuthVerifyIfGiven:
		return tls.VerifyClientCertIfGiven
	case config.ClientAuthRequireAndVerify:
		return tls.RequireAndVerifyClientCert
	default:
		return tls.NoClientCert
	}
}

// Implement Receiver interface methods by delegating to embedded host

// Attach implements relay.AttachableReceiver
func (s *ServerService) Attach(p pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return nil, ErrServerHostNotInitialized
	}

	return s.host.Attach(p, ch)
}

// Detach implements relay.AttachableReceiver
func (s *ServerService) Detach(p pid.PID) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return
	}

	s.host.Detach(p)
}

// Send implements relay.Receiver
func (s *ServerService) Send(pkg *relay.Package) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return ErrServerHostNotInitialized
	}

	return s.host.Send(pkg)
}

// Ensure ServerService implements required interfaces
var (
	_ supervisor.Service = (*ServerService)(nil)
	_ Server             = (*ServerService)(nil)
)
