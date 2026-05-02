// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	config "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

// --- Test doubles -----------------------------------------------------------

// overlayService is a netapi.Service that binds on the host's loopback so
// tests can actually connect through it. Listen/ListenPacket use the normal
// kernel stack while still exercising the overlay code path in the server.
type overlayService struct {
	listenErr error

	mu        sync.Mutex
	dialCalls int
	listenHit int
}

func (s *overlayService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	s.mu.Lock()
	s.dialCalls++
	s.mu.Unlock()
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func (s *overlayService) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	s.mu.Lock()
	s.listenHit++
	s.mu.Unlock()
	if s.listenErr != nil {
		return nil, s.listenErr
	}
	return (&net.ListenConfig{}).Listen(ctx, network, address)
}

func (s *overlayService) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return (&net.ListenConfig{}).ListenPacket(ctx, network, address)
}

func (s *overlayService) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func (s *overlayService) dials() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dialCalls
}

func (s *overlayService) listens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listenHit
}

// overlayTLSService is an overlayService that also implements netapi.TLSListener.
// ListenTLS returns a plain listener for the auto-TLS path test; we don't need
// a real cert because we only assert the code took the auto branch.
type overlayTLSService struct {
	overlayService
}

func (s *overlayTLSService) ListenTLS(ctx context.Context, network, address string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(ctx, network, address)
}

// mockNetRegistry implements netapi.NetworkRegistry.
type mockNetRegistry struct {
	services map[string]netapi.Service
	kinds    map[string]registry.Kind
}

func newMockNetRegistry() *mockNetRegistry {
	return &mockNetRegistry{
		services: make(map[string]netapi.Service),
		kinds:    make(map[string]registry.Kind),
	}
}

func (r *mockNetRegistry) register(id string, svc netapi.Service, kind registry.Kind) {
	r.services[id] = svc
	r.kinds[id] = kind
}

func (r *mockNetRegistry) GetNetwork(id registry.ID) (netapi.Service, error) {
	svc, ok := r.services[id.String()]
	if !ok {
		return nil, netapi.ErrNetworkNotFound
	}
	return svc, nil
}

func (r *mockNetRegistry) HasNetwork(id registry.ID) bool {
	_, ok := r.services[id.String()]
	return ok
}

func (r *mockNetRegistry) NetworkKind(id registry.ID) registry.Kind {
	return r.kinds[id.String()]
}

// --- helpers ---------------------------------------------------------------

func overlayCtx() context.Context {
	ctx := ctxapi.NewRootContext()
	// Tests don't construct a full security actor/scope; switch off strict
	// mode so IsAllowed returns true on missing security context.
	secapi.SetStrictMode(ctx, false)
	return ctx
}

func overlayCtxWithRegistry(reg netapi.NetworkRegistry) context.Context {
	ctx := overlayCtx()
	ctx = netapi.WithNetworkRegistry(ctx, reg)
	return ctx
}

// makeServer creates a ServerService with the given config for buildListener tests.
func makeServer(t *testing.T, cfg *config.ServerConfig) *ServerService {
	t.Helper()
	s, err := NewServerService(registry.NewID("test", "server-overlay"), cfg, NewMiddlewareRegistry(zap.NewNop()))
	require.NoError(t, err)
	return s
}

// selfSignedPEM returns a throwaway self-signed RSA cert + private key as
// PEM-encoded byte slices. The cert is valid for 127.0.0.1 so clients can
// actually complete a TLS handshake against it in-process.
func selfSignedPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	iss := newCA(t, "test-server")
	return iss.pem, iss.keyPEM
}

// caMaterial bundles CA-signed artifacts used across the mTLS tests.
type caMaterial struct {
	serverCert  []byte
	serverKey   []byte
	clientCert  []byte
	clientKey   []byte
	unknownCert []byte
	unknownKey  []byte
	caCertPEM   []byte
}

// issuer holds a CA's certificate + key along with PEM encodings and a
// template suitable for signing leaf certs.
type issuer struct {
	cert   *x509.Certificate
	key    *rsa.PrivateKey
	pem    []byte
	keyPEM []byte
}

func newCA(t *testing.T, cn string) *issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return &issuer{
		cert:   cert,
		key:    key,
		pem:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}
}

// issueLeaf signs a leaf cert under the given CA (or self-signs when parent
// is nil). usage selects ExtKeyUsage (server or client auth).
func issueLeaf(t *testing.T, cn string, parent *issuer, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	parentCert := tmpl
	parentKey := leafKey
	if parent != nil {
		parentCert = parent.cert
		parentKey = parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &leafKey.PublicKey, parentKey)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
	return certPEM, keyPEM
}

// newCAMaterial builds a CA, a server cert signed by it, a trusted client
// cert signed by it, and an unknown client cert signed by a second CA (so
// RequireAndVerifyClientCert has something to reject).
func newCAMaterial(t *testing.T) caMaterial {
	t.Helper()

	trustedCA := newCA(t, "trusted-ca")
	srvCert, srvKey := issueLeaf(t, "test-server", trustedCA, x509.ExtKeyUsageServerAuth)
	cliCert, cliKey := issueLeaf(t, "trusted-client", trustedCA, x509.ExtKeyUsageClientAuth)

	unknownCA := newCA(t, "unknown-ca")
	unknownCert, unknownKey := issueLeaf(t, "rogue-client", unknownCA, x509.ExtKeyUsageClientAuth)

	return caMaterial{
		serverCert:  srvCert,
		serverKey:   srvKey,
		clientCert:  cliCert,
		clientKey:   cliKey,
		unknownCert: unknownCert,
		unknownKey:  unknownKey,
		caCertPEM:   trustedCA.pem,
	}
}

// testEnvRegistry is a minimal envapi.Registry shim that only needs Get.
type testEnvRegistry struct {
	values map[string]string
}

func (r *testEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if v, ok := r.values[name]; ok {
		return v, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (r *testEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	v, ok := r.values[name]
	if !ok {
		return "", false, envapi.ErrVariableNotFound
	}
	return v, true, nil
}

func (r *testEnvRegistry) Set(context.Context, string, string) error { return nil }
func (r *testEnvRegistry) All(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *testEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}
func (r *testEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}

// --- buildListener --------------------------------------------------------

func TestBuildListener_Clearnet_Plain(t *testing.T) {
	cfg := &config.ServerConfig{Addr: "127.0.0.1:0"}
	s := makeServer(t, cfg)

	ln, probe, err := s.buildListener(overlayCtx())
	require.NoError(t, err)
	require.NotNil(t, ln)
	require.NotNil(t, probe)
	t.Cleanup(func() { _ = ln.Close() })

	// Clearnet probe should succeed against the bound address.
	addr := ln.Addr().String()
	conn, err := probe(context.Background(), addr)
	require.NoError(t, err)
	_ = conn.Close()
}

func TestBuildListener_Clearnet_RejectsAutoTLS(t *testing.T) {
	cfg := &config.ServerConfig{
		Addr: "127.0.0.1:0",
		TLS:  config.ServerTLSConfig{Mode: config.TLSModeAuto},
	}
	s := makeServer(t, cfg)

	_, _, err := s.buildListener(overlayCtx())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClearnetAutoTLSUnsupported)
}

func TestBuildListener_Clearnet_ManualTLS(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	cfg := &config.ServerConfig{
		Addr: "127.0.0.1:0",
		TLS: config.ServerTLSConfig{
			Mode: config.TLSModeManual,
			Cert: string(certPEM),
			Key:  string(keyPEM),
		},
	}
	s := makeServer(t, cfg)

	ln, _, err := s.buildListener(overlayCtx())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	require.NotNil(t, ln)
}

func TestBuildListener_Overlay_NoRegistry(t *testing.T) {
	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
	}
	s := makeServer(t, cfg)

	_, _, err := s.buildListener(overlayCtx())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNetworkRegistryNotAvailable)
}

func TestBuildListener_Overlay_ResolveError(t *testing.T) {
	reg := newMockNetRegistry()
	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "missing"),
	}
	s := makeServer(t, cfg)

	_, _, err := s.buildListener(overlayCtxWithRegistry(reg))
	require.Error(t, err)
	// The resolve error wraps ErrNetworkNotFound from the registry.
	assert.ErrorIs(t, err, netapi.ErrNetworkNotFound)
}

func TestBuildListener_Overlay_PlainTLSOff(t *testing.T) {
	svc := &overlayService{}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
	}
	s := makeServer(t, cfg)

	ctx := overlayCtxWithRegistry(reg)
	ln, probe, err := s.buildListener(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	assert.Equal(t, 1, svc.listens(), "driver Listen should have been called")

	// Overlay probe must go through the driver, not kernel.
	addr := ln.Addr().String()
	conn, err := probe(context.Background(), addr)
	require.NoError(t, err)
	_ = conn.Close()
	assert.GreaterOrEqual(t, svc.dials(), 1, "probe must dial through the driver")
}

func TestBuildListener_Overlay_AutoTLS_Supported(t *testing.T) {
	svc := &overlayTLSService{}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
		TLS:     config.ServerTLSConfig{Mode: config.TLSModeAuto},
	}
	s := makeServer(t, cfg)

	ln, _, err := s.buildListener(overlayCtxWithRegistry(reg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	// overlayTLSService's plain Listen must NOT have been hit — auto path
	// uses ListenTLS.
	assert.Equal(t, 0, svc.listens())
}

func TestBuildListener_Overlay_AutoTLS_Unsupported(t *testing.T) {
	svc := &overlayService{} // no TLSListener impl
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.i2p"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
		TLS:     config.ServerTLSConfig{Mode: config.TLSModeAuto},
	}
	s := makeServer(t, cfg)

	_, _, err := s.buildListener(overlayCtxWithRegistry(reg))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto TLS")
}

func TestBuildListener_Overlay_ManualTLS(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	svc := &overlayService{}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
		TLS: config.ServerTLSConfig{
			Mode: config.TLSModeManual,
			Cert: string(certPEM),
			Key:  string(keyPEM),
		},
	}
	s := makeServer(t, cfg)

	ln, _, err := s.buildListener(overlayCtxWithRegistry(reg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	assert.Equal(t, 1, svc.listens(), "manual TLS must wrap driver.Listen")
}

func TestBuildListener_Overlay_BindDeniedInStrictMode(t *testing.T) {
	svc := &overlayService{}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
	}
	s := makeServer(t, cfg)

	// Build a strict-mode context without a security actor/scope. The
	// permission gate must deny network.bind.
	ctx := ctxapi.NewRootContext()
	ctx = netapi.WithNetworkRegistry(ctx, reg)
	// Strict mode is default; assert explicitly for clarity.
	secapi.SetStrictMode(ctx, true)

	_, _, err := s.buildListener(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
	assert.Equal(t, 0, svc.listens(), "denied bind must not reach driver")
}

func TestBuildListener_Overlay_ListenFailure(t *testing.T) {
	svc := &overlayService{listenErr: errors.New("overlay down")}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
	}
	s := makeServer(t, cfg)

	_, _, err := s.buildListener(overlayCtxWithRegistry(reg))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overlay listen failed")
}

// --- probeFunc wiring integration ----------------------------------------

// TestServer_OverlayProbe_UsesDriver exercises Start() end-to-end with an
// overlay driver and asserts the readiness probe went through the overlay's
// DialContext, not the kernel dialer.
func TestServer_OverlayProbe_UsesDriver(t *testing.T) {
	svc := &overlayService{}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
		Timeouts: config.TimeoutConfig{
			ReadTimeout:  time.Second,
			WriteTimeout: time.Second,
			IdleTimeout:  time.Second,
		},
	}

	// Pre-resolve a concrete port so the probe's target matches the bind.
	port, err := findFreePort()
	require.NoError(t, err)
	cfg.Addr = fmt.Sprintf("127.0.0.1:%d", port)

	s := makeServer(t, cfg)
	ctx, cancel := context.WithCancel(overlayCtxWithRegistry(reg))
	defer cancel()

	ch, err := s.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = s.Stop(context.Background())
	})

	// Drain status until listening message arrives or timeout.
	deadline := time.After(5 * time.Second)
	var listening atomic.Bool
loop:
	for {
		select {
		case msg := <-ch:
			if str, ok := msg.(string); ok && len(str) > 0 {
				listening.Store(true)
				break loop
			}
		case <-deadline:
			t.Fatalf("server did not report listening")
		}
	}

	assert.True(t, listening.Load())
	assert.GreaterOrEqual(t, svc.dials(), 1, "readiness probe must dial through overlay")
	assert.Equal(t, 1, svc.listens(), "server must bind through overlay")
}

// --- loadServerTLSConfig unit tests --------------------------------------

func TestLoadServerTLSConfig_InlineCert(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	cfg := config.ServerTLSConfig{
		Mode: config.TLSModeManual,
		Cert: string(certPEM),
		Key:  string(keyPEM),
	}

	out, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Len(t, out.Certificates, 1)
	assert.Equal(t, uint16(tls.VersionTLS12), out.MinVersion)
	assert.Equal(t, tls.NoClientCert, out.ClientAuth)
	assert.Nil(t, out.ClientCAs)
}

func TestLoadServerTLSConfig_EnvCert(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)

	ctx := overlayCtx()
	ctx = envapi.WithRegistry(ctx, &testEnvRegistry{
		values: map[string]string{
			"app.env:tls_cert": string(certPEM),
			"app.env:tls_key":  string(keyPEM),
		},
	})

	cfg := config.ServerTLSConfig{
		Mode:    config.TLSModeManual,
		CertEnv: "app.env:tls_cert",
		KeyEnv:  "app.env:tls_key",
	}

	out, err := loadServerTLSConfig(ctx, cfg)
	require.NoError(t, err)
	require.Len(t, out.Certificates, 1)
}

func TestLoadServerTLSConfig_EnvRegistryMissing(t *testing.T) {
	cfg := config.ServerTLSConfig{
		Mode:    config.TLSModeManual,
		CertEnv: "app.env:tls_cert",
		KeyEnv:  "app.env:tls_key",
	}
	_, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTLSEnvRegistryUnavailable)
}

func TestLoadServerTLSConfig_EnvLookupFails(t *testing.T) {
	ctx := overlayCtx()
	ctx = envapi.WithRegistry(ctx, &testEnvRegistry{values: map[string]string{}})
	cfg := config.ServerTLSConfig{
		Mode:    config.TLSModeManual,
		CertEnv: "app.env:missing",
		KeyEnv:  "app.env:missing2",
	}
	_, err := loadServerTLSConfig(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve TLS env variable")
}

func TestLoadServerTLSConfig_InvalidPEM(t *testing.T) {
	cfg := config.ServerTLSConfig{
		Mode: config.TLSModeManual,
		Cert: "not a pem",
		Key:  "neither",
	}
	_, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load TLS")
}

func TestLoadServerTLSConfig_ClientCAInline(t *testing.T) {
	m := newCAMaterial(t)
	cfg := config.ServerTLSConfig{
		Mode:       config.TLSModeManual,
		Cert:       string(m.serverCert),
		Key:        string(m.serverKey),
		ClientCA:   string(m.caCertPEM),
		ClientAuth: config.ClientAuthRequireAndVerify,
	}
	out, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.NoError(t, err)
	require.NotNil(t, out.ClientCAs)
	assert.Equal(t, tls.RequireAndVerifyClientCert, out.ClientAuth)
}

func TestLoadServerTLSConfig_ClientCAEnv(t *testing.T) {
	m := newCAMaterial(t)
	ctx := overlayCtx()
	ctx = envapi.WithRegistry(ctx, &testEnvRegistry{
		values: map[string]string{"app.env:ca": string(m.caCertPEM)},
	})
	cfg := config.ServerTLSConfig{
		Mode:        config.TLSModeManual,
		Cert:        string(m.serverCert),
		Key:         string(m.serverKey),
		ClientCAEnv: "app.env:ca",
		ClientAuth:  config.ClientAuthVerifyIfGiven,
	}
	out, err := loadServerTLSConfig(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, out.ClientCAs)
	assert.Equal(t, tls.VerifyClientCertIfGiven, out.ClientAuth)
}

func TestLoadServerTLSConfig_ClientCAInvalid(t *testing.T) {
	m := newCAMaterial(t)
	cfg := config.ServerTLSConfig{
		Mode:       config.TLSModeManual,
		Cert:       string(m.serverCert),
		Key:        string(m.serverKey),
		ClientCA:   "garbage",
		ClientAuth: config.ClientAuthRequireAndVerify,
	}
	_, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid certificates")
}

func TestMapClientAuthType(t *testing.T) {
	cases := []struct {
		in   config.ClientAuthType
		want tls.ClientAuthType
	}{
		{config.ClientAuthNone, tls.NoClientCert},
		{config.ClientAuthRequest, tls.RequestClientCert},
		{config.ClientAuthRequireAny, tls.RequireAnyClientCert},
		{config.ClientAuthVerifyIfGiven, tls.VerifyClientCertIfGiven},
		{config.ClientAuthRequireAndVerify, tls.RequireAndVerifyClientCert},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, mapClientAuthType(tc.in), string(tc.in))
	}
}

// --- mTLS handshake end-to-end ------------------------------------------

// serveTLSOnce starts a TLS server that accepts one connection and
// responds "ok". Returns the address and a cleanup func.
func serveTLSOnce(t *testing.T, tlsCfg *tls.Config) (addr string, done func()) {
	t.Helper()
	raw, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln := tls.NewListener(raw, tlsCfg)
	addr = ln.Addr().String()

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if tc, ok := conn.(*tls.Conn); ok {
			if err := tc.HandshakeContext(context.Background()); err != nil {
				_ = conn.Close()
				return
			}
		}
		_, _ = conn.Write([]byte("ok"))
		_ = conn.Close()
	}()

	return addr, func() {
		_ = ln.Close()
		<-doneCh
	}
}

// requireTLSHandshakeFails asserts the client-side mTLS handshake fails at
// some point — either immediately during Dial (TLS 1.2) or on the first
// Read/Write of application data (TLS 1.3 post-handshake alert, since the
// server only validates the client cert after the initial flight).
func requireTLSHandshakeFails(t *testing.T, addr string, clientCfg *tls.Config, msg string) {
	t.Helper()
	conn, err := (&tls.Dialer{Config: clientCfg}).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return
	}
	defer conn.Close()
	_, werr := conn.Write([]byte("ping"))
	if werr == nil {
		buf := make([]byte, 4)
		_, werr = conn.Read(buf)
	}
	require.Error(t, werr, msg)
}

func TestLoadServerTLSConfig_MTLS_RequireAndVerify_Rejects(t *testing.T) {
	m := newCAMaterial(t)
	cfg := config.ServerTLSConfig{
		Mode:       config.TLSModeManual,
		Cert:       string(m.serverCert),
		Key:        string(m.serverKey),
		ClientCA:   string(m.caCertPEM),
		ClientAuth: config.ClientAuthRequireAndVerify,
	}
	srvCfg, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.NoError(t, err)

	addr, done := serveTLSOnce(t, srvCfg)
	defer done()

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(m.caCertPEM)

	requireTLSHandshakeFails(t, addr, &tls.Config{
		RootCAs:    rootPool,
		ServerName: "127.0.0.1",
	}, "handshake must fail without client cert")
}

func TestLoadServerTLSConfig_MTLS_RequireAndVerify_Accepts(t *testing.T) {
	m := newCAMaterial(t)
	cfg := config.ServerTLSConfig{
		Mode:       config.TLSModeManual,
		Cert:       string(m.serverCert),
		Key:        string(m.serverKey),
		ClientCA:   string(m.caCertPEM),
		ClientAuth: config.ClientAuthRequireAndVerify,
	}
	srvCfg, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.NoError(t, err)

	addr, done := serveTLSOnce(t, srvCfg)
	defer done()

	clientKP, err := tls.X509KeyPair(m.clientCert, m.clientKey)
	require.NoError(t, err)

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(m.caCertPEM)

	conn, err := (&tls.Dialer{Config: &tls.Config{
		RootCAs:      rootPool,
		Certificates: []tls.Certificate{clientKP},
		ServerName:   "127.0.0.1",
	}}).DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	buf := make([]byte, 2)
	_, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(buf))
}

func TestLoadServerTLSConfig_MTLS_RejectsUnknownCA(t *testing.T) {
	m := newCAMaterial(t)
	cfg := config.ServerTLSConfig{
		Mode:       config.TLSModeManual,
		Cert:       string(m.serverCert),
		Key:        string(m.serverKey),
		ClientCA:   string(m.caCertPEM),
		ClientAuth: config.ClientAuthRequireAndVerify,
	}
	srvCfg, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.NoError(t, err)

	addr, done := serveTLSOnce(t, srvCfg)
	defer done()

	unknownKP, err := tls.X509KeyPair(m.unknownCert, m.unknownKey)
	require.NoError(t, err)

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(m.caCertPEM)

	requireTLSHandshakeFails(t, addr, &tls.Config{
		RootCAs:      rootPool,
		Certificates: []tls.Certificate{unknownKP},
		ServerName:   "127.0.0.1",
	}, "handshake must fail for client cert signed by unknown CA")
}

func TestLoadServerTLSConfig_MTLS_VerifyIfGiven_AcceptsNoCert(t *testing.T) {
	m := newCAMaterial(t)
	cfg := config.ServerTLSConfig{
		Mode:       config.TLSModeManual,
		Cert:       string(m.serverCert),
		Key:        string(m.serverKey),
		ClientCA:   string(m.caCertPEM),
		ClientAuth: config.ClientAuthVerifyIfGiven,
	}
	srvCfg, err := loadServerTLSConfig(overlayCtx(), cfg)
	require.NoError(t, err)

	addr, done := serveTLSOnce(t, srvCfg)
	defer done()

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(m.caCertPEM)

	conn, err := (&tls.Dialer{Config: &tls.Config{RootCAs: rootPool, ServerName: "127.0.0.1"}}).DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	buf := make([]byte, 2)
	_, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(buf))
}

// TestLoadServerTLSConfig_ClientAuthRequest_NoCA verifies that Request and
// RequireAny auth modes load without a ClientCAs pool — Go's TLS does not
// verify against a pool in those modes.
func TestLoadServerTLSConfig_ClientAuthRequest_NoCA(t *testing.T) {
	cases := []struct {
		name string
		mode config.ClientAuthType
		want tls.ClientAuthType
	}{
		{"request", config.ClientAuthRequest, tls.RequestClientCert},
		{"require_any", config.ClientAuthRequireAny, tls.RequireAnyClientCert},
	}
	certPEM, keyPEM := selfSignedPEM(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.ServerTLSConfig{
				Mode:       config.TLSModeManual,
				Cert:       string(certPEM),
				Key:        string(keyPEM),
				ClientAuth: tc.mode,
			}
			out, err := loadServerTLSConfig(overlayCtx(), cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.want, out.ClientAuth)
			assert.Nil(t, out.ClientCAs, "no pool expected when no CA configured")
		})
	}
}

// TestBuildListener_Clearnet_ManualTLS_Handshake exercises the clearnet
// path end-to-end: build a TLS listener via buildListener, serve one
// request, and verify the client completes a handshake against it.
func TestBuildListener_Clearnet_ManualTLS_Handshake(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	cfg := &config.ServerConfig{
		Addr: "127.0.0.1:0",
		TLS: config.ServerTLSConfig{
			Mode: config.TLSModeManual,
			Cert: string(certPEM),
			Key:  string(keyPEM),
		},
	}
	s := makeServer(t, cfg)

	ln, _, err := s.buildListener(overlayCtx())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("hi"))
		_ = conn.Close()
	}()

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(certPEM)

	conn, err := (&tls.Dialer{Config: &tls.Config{RootCAs: rootPool, ServerName: "127.0.0.1"}}).DialContext(context.Background(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	buf := make([]byte, 2)
	_, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hi", string(buf))
}

// TestBuildListener_Clearnet_ManualTLS_EnvCert covers the env-resolved
// cert path through the full buildListener flow.
func TestBuildListener_Clearnet_ManualTLS_EnvCert(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)

	cfg := &config.ServerConfig{
		Addr: "127.0.0.1:0",
		TLS: config.ServerTLSConfig{
			Mode:    config.TLSModeManual,
			CertEnv: "app.env:tls_cert",
			KeyEnv:  "app.env:tls_key",
		},
	}
	s := makeServer(t, cfg)

	ctx := envapi.WithRegistry(overlayCtx(), &testEnvRegistry{
		values: map[string]string{
			"app.env:tls_cert": string(certPEM),
			"app.env:tls_key":  string(keyPEM),
		},
	})

	ln, _, err := s.buildListener(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
}

// TestBuildListener_Overlay_ManualTLS_MTLS combines overlay + mTLS: the
// listener should wrap the driver.Listen result in a tls.Listener and
// reject clients missing a trusted cert.
func TestBuildListener_Overlay_ManualTLS_MTLS(t *testing.T) {
	m := newCAMaterial(t)
	svc := &overlayService{}
	reg := newMockNetRegistry()
	reg.register("app.net:overlay", svc, registry.Kind("net.tailscale"))

	cfg := &config.ServerConfig{
		Addr:    "127.0.0.1:0",
		Network: registry.NewID("app.net", "overlay"),
		TLS: config.ServerTLSConfig{
			Mode:       config.TLSModeManual,
			Cert:       string(m.serverCert),
			Key:        string(m.serverKey),
			ClientCA:   string(m.caCertPEM),
			ClientAuth: config.ClientAuthRequireAndVerify,
		},
	}
	s := makeServer(t, cfg)

	ln, _, err := s.buildListener(overlayCtxWithRegistry(reg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	assert.Equal(t, 1, svc.listens())

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if tc, ok := conn.(*tls.Conn); ok {
			_ = tc.HandshakeContext(context.Background())
		}
		_ = conn.Close()
	}()

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(m.caCertPEM)

	requireTLSHandshakeFails(t, ln.Addr().String(), &tls.Config{
		RootCAs:    rootPool,
		ServerName: "127.0.0.1",
	}, "overlay mTLS must reject client without cert")
}

// TestServer_Start_ManualTLS boots the full Start() flow with a manual-TLS
// config and verifies clients can complete a TLS handshake against the
// running server — exercising the readiness probe + TLS wrapping.
func TestServer_Start_ManualTLS(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)

	port, err := findFreePort()
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
		TLS: config.ServerTLSConfig{
			Mode: config.TLSModeManual,
			Cert: string(certPEM),
			Key:  string(keyPEM),
		},
		Timeouts: config.TimeoutConfig{
			ReadTimeout:  time.Second,
			WriteTimeout: time.Second,
			IdleTimeout:  time.Second,
		},
	}

	s := makeServer(t, cfg)
	s.SetHandlerFunc(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	ctx, cancel := context.WithCancel(overlayCtx())
	defer cancel()

	ch, err := s.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Wait for the "listening on" status message.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-ch:
			if _, ok := msg.(string); ok {
				goto connected
			}
		case <-deadline:
			t.Fatal("server did not report listening")
		}
	}
connected:

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(certPEM)

	dialer := &net.Dialer{Timeout: 2 * time.Second}
	raw, err := dialer.Dial("tcp", cfg.Addr)
	require.NoError(t, err)
	tconn := tls.Client(raw, &tls.Config{RootCAs: rootPool, ServerName: "127.0.0.1"})
	require.NoError(t, tconn.HandshakeContext(context.Background()))
	_ = tconn.Close()
}
