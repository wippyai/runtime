// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	apimetrics "github.com/wippyai/runtime/api/metrics"
	apipayload "github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

// generateTestTLSCerts creates a temporary CA and two leaf certificates signed by that CA.
// All certificate fixtures are created in t.TempDir() and are temporary only.
func generateTestTLSCerts(t *testing.T, nameA, nameB string) (internode.ManagerTLSConfig, internode.ManagerTLSConfig) {
	t.Helper()

	rootPub, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	now := time.Now()
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	rootDer, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, rootPub, rootPriv)
	require.NoError(t, err)

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDer}), 0600))

	issueLeaf := func(name string, serial int64) internode.ManagerTLSConfig {
		leafPub, leafPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		leafTemplate := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			NotBefore:    now.Add(-time.Minute),
			NotAfter:     now.Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
			DNSNames:     []string{"localhost"},
		}

		leafDer, err := x509.CreateCertificate(rand.Reader, leafTemplate, rootTemplate, leafPub, rootPriv)
		require.NoError(t, err)

		keyBytes, err := x509.MarshalPKCS8PrivateKey(leafPriv)
		require.NoError(t, err)

		certPath := filepath.Join(dir, name+".pem")
		keyPath := filepath.Join(dir, name+".key")

		require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDer}), 0600))
		require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0600))

		return internode.ManagerTLSConfig{
			Enabled:  true,
			CAFile:   caPath,
			CertFile: certPath,
			KeyFile:  keyPath,
		}
	}

	return issueLeaf(nameA, 2), issueLeaf(nameB, 3)
}

type twoStackEnv struct {
	collector apimetrics.Collector
	trusted   map[string]string
	secret    []byte
	privA     ed25519.PrivateKey
	privB     ed25519.PrivateKey
	pubA      ed25519.PublicKey
	pubB      ed25519.PublicKey
}

func setupTwoStackEnv(t *testing.T) *twoStackEnv {
	t.Helper()

	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)

	collector := metrics.NewCollector(metricscfg.Config{})
	t.Cleanup(func() { collector.Close() })

	trusted := map[string]string{
		"node-a": base64.RawStdEncoding.EncodeToString(pubA),
		"node-b": base64.RawStdEncoding.EncodeToString(pubB),
	}

	return &twoStackEnv{
		collector: collector,
		secret:    secret,
		privA:     privA,
		privB:     privB,
		pubA:      pubA,
		pubB:      pubB,
		trusted:   trusted,
	}
}

func (env *twoStackEnv) config(name string, key ed25519.PrivateKey) StackConfig {
	return StackConfig{
		NodeName:                 name,
		Logger:                   zap.NewNop(),
		Bus:                      eventbus.NewBus(),
		Transcoder:               payload.NewTranscoder(),
		Collector:                env.collector,
		MembershipBindAddr:       "127.0.0.1",
		InternodeBindAddr:        "127.0.0.1",
		MembershipBindPort:       0,
		InternodeBindPort:        0,
		InternodeAutoPort:        true,
		SecretKey:                base64.StdEncoding.EncodeToString(env.secret),
		InternodeIdentityKey:     base64.RawStdEncoding.EncodeToString(key),
		InternodeTrustedPeerKeys: env.trusted,
	}
}

func TestStackTLSInvalidConfigurationFails(t *testing.T) {
	env := setupTwoStackEnv(t)
	tempDir := t.TempDir()

	testCases := []struct {
		name      string
		tlsConfig internode.ManagerTLSConfig
	}{
		{
			name: "missing cert file",
			tlsConfig: internode.ManagerTLSConfig{
				Enabled:  true,
				CertFile: filepath.Join(tempDir, "nonexistent-cert.pem"),
				KeyFile:  filepath.Join(tempDir, "nonexistent-key.pem"),
				CAFile:   filepath.Join(tempDir, "nonexistent-ca.pem"),
			},
		},
		{
			name: "missing key file",
			tlsConfig: func() internode.ManagerTLSConfig {
				c, _ := generateTestTLSCerts(t, "node-a", "node-b")
				c.KeyFile = filepath.Join(tempDir, "nonexistent-key.pem")
				return c
			}(),
		},
		{
			name: "missing ca file",
			tlsConfig: func() internode.ManagerTLSConfig {
				c, _ := generateTestTLSCerts(t, "node-a", "node-b")
				c.CAFile = filepath.Join(tempDir, "nonexistent-ca.pem")
				return c
			}(),
		},
		{
			name: "corrupted ca file",
			tlsConfig: func() internode.ManagerTLSConfig {
				c, _ := generateTestTLSCerts(t, "node-a", "node-b")
				badCA := filepath.Join(tempDir, "corrupt-ca.pem")
				require.NoError(t, os.WriteFile(badCA, []byte("NOT A VALID CERTIFICATE"), 0600))
				c.CAFile = badCA
				return c
			}(),
		},
		{
			name: "corrupted cert file",
			tlsConfig: func() internode.ManagerTLSConfig {
				c, _ := generateTestTLSCerts(t, "node-a", "node-b")
				badCert := filepath.Join(tempDir, "corrupt-cert.pem")
				require.NoError(t, os.WriteFile(badCert, []byte("NOT A VALID CERTIFICATE"), 0600))
				c.CertFile = badCert
				return c
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := env.config("node-a", env.privA)
			cfg.InternodeTLS = tc.tlsConfig

			// Construction succeeds because construction must not load TLS or bind ports
			stack, err := AssembleStack(cfg)
			require.NoError(t, err)

			// Startup must fail because TLS configuration cannot be loaded
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err = stack.Start(ctx)
			require.Error(t, err, "Start must fail when TLS configuration is invalid")
			_ = stack.Stop()
		})
	}
}

func TestTwoStackTLSCommunication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	env := setupTwoStackEnv(t)
	tlsA, tlsB := generateTestTLSCerts(t, "node-a", "node-b")

	cfgA := env.config("node-a", env.privA)
	cfgA.InternodeTLS = tlsA

	stackA, err := AssembleStack(cfgA)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stackA.Stop() })

	require.NoError(t, stackA.Start(ctx))

	cfgB := env.config("node-b", env.privB)
	cfgB.InternodeTLS = tlsB
	cfgB.JoinAddrs = []string{stackA.Membership.LocalNode().Addr}

	stackB, err := AssembleStack(cfgB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stackB.Stop() })

	require.NoError(t, stackB.Start(ctx))

	// Verify both nodes form an authenticated TLS mesh connection
	require.Eventually(t, func() bool {
		return len(stackA.ConnMgr.ConnectedNodes()) == 1 && len(stackB.ConnMgr.ConnectedNodes()) == 1
	}, 15*time.Second, 50*time.Millisecond, "mutual TLS mesh connection must form between two stacks")

	require.Equal(t, []clusterapi.NodeID{"node-b"}, stackA.ConnMgr.ConnectedNodes())
	require.Equal(t, []clusterapi.NodeID{"node-a"}, stackB.ConnMgr.ConnectedNodes())

	// Inspect an actual decoded relay message, not a copied configuration flag.
	received := make(chan tlsMessage, 1)
	require.NoError(t, stackB.Node.RegisterHost("tls-proof", tlsInbox{received}))
	testPayload := []byte("hello-over-native-tls")
	pkg := relay.NewServicePackage("node-a", "fixture", "node-b", "tls-proof", "proof", apipayload.NewPayload(testPayload, apipayload.Bytes))
	if err := stackA.Router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		t.Fatal(err)
	}
	select {
	case message := <-received:
		require.Equal(t, testPayload, message.body)
	case <-ctx.Done():
		t.Fatal("TLS relay message not received", ctx.Err())
	}
}

type tlsMessage struct {
	body []byte
}
type tlsInbox struct{ received chan tlsMessage }

func (r tlsInbox) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		if len(msg.Payloads) != 1 {
			return errors.New("unexpected TLS payload count")
		}
		body, ok := msg.Payloads[0].Data().([]byte)
		if !ok {
			return errors.New("unexpected TLS payload type")
		}
		select {
		case r.received <- tlsMessage{body: append([]byte(nil), body...)}:
		default:
			return errors.New("unexpected TLS duplicate")
		}
	}
	return nil
}

func TestTwoStackTLSWithPlaintextIncompatible(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTwoStackEnv(t)
	tlsA, _ := generateTestTLSCerts(t, "node-a", "node-b")

	// Stack A has native TLS enabled
	cfgA := env.config("node-a", env.privA)
	cfgA.InternodeTLS = tlsA

	stackA, err := AssembleStack(cfgA)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stackA.Stop() })
	require.NoError(t, stackA.Start(ctx))

	// Stack B has native TLS disabled (plaintext)
	cfgB := env.config("node-b", env.privB)
	cfgB.InternodeTLS = internode.ManagerTLSConfig{Enabled: false}
	cfgB.JoinAddrs = []string{stackA.Membership.LocalNode().Addr}

	stackB, err := AssembleStack(cfgB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stackB.Stop() })
	require.NoError(t, stackB.Start(ctx))

	// Stacks will discover each other over gossip, but TLS handshake will fail
	// because Stack B dials with plaintext TCP while Stack A expects TLS.
	// Therefore, mesh connection must never form.
	require.Never(t, func() bool {
		return len(stackA.ConnMgr.ConnectedNodes()) > 0 || len(stackB.ConnMgr.ConnectedNodes()) > 0
	}, 3*time.Second, 100*time.Millisecond, "plaintext stack must not form internode connection with TLS stack")
}

func TestTwoStackTLSWithUntrustedCAFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTwoStackEnv(t)

	// Generate two separate, independent CAs
	tlsA1, _ := generateTestTLSCerts(t, "node-a", "node-b")
	_, tlsB2 := generateTestTLSCerts(t, "node-a", "node-b")

	// Stack A trusts CA 1
	cfgA := env.config("node-a", env.privA)
	cfgA.InternodeTLS = tlsA1

	stackA, err := AssembleStack(cfgA)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stackA.Stop() })
	require.NoError(t, stackA.Start(ctx))

	// Stack B trusts CA 2 (different root CA)
	cfgB := env.config("node-b", env.privB)
	cfgB.InternodeTLS = tlsB2
	cfgB.JoinAddrs = []string{stackA.Membership.LocalNode().Addr}

	stackB, err := AssembleStack(cfgB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stackB.Stop() })
	require.NoError(t, stackB.Start(ctx))

	// Due to mutual TLS verification failure, connection must never form
	require.Never(t, func() bool {
		return len(stackA.ConnMgr.ConnectedNodes()) > 0 || len(stackB.ConnMgr.ConnectedNodes()) > 0
	}, 3*time.Second, 100*time.Millisecond, "stacks with untrusted CAs must not form connection")
}
