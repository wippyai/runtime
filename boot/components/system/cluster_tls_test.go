// SPDX-License-Identifier: MPL-2.0
package system

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logsapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	payloadapi "github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

func TestClusterBootTLSConfigurationFailsClosed(t *testing.T) {
	for name, values := range map[string]map[string]any{
		"string-enabled":   {"enabled": "true"},
		"unknown":          {"verify": false},
		"missing-paths":    {"enabled": true},
		"disabled-paths":   {"enabled": false, "cert_file": "cert"},
		"implicit-enabled": {"cert_file": "cert"},
		"empty-path":       {"enabled": true, "cert_file": ""},
		"nonstring-path":   {"enabled": true, "key_file": 1},
	} {
		t.Run(name, func(t *testing.T) {
			settings := make(map[string]any)
			for key, value := range values {
				settings["internode.tls."+key] = value
			}
			_, err := clusterTLSConfig(boot.NewConfig(boot.WithSection("cluster", settings)).Sub("cluster"))
			require.Error(t, err)
		})
	}
	config, err := clusterTLSConfig(boot.NewConfig().Sub("cluster"))
	require.NoError(t, err)
	require.False(t, config.Enabled)
}

func TestClusterBootTLSUsesNativeManager(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "mutual-tls"
		if invalid {
			name = "invalid-certificates"
		}
		t.Run(name, func(t *testing.T) {
			pub, key, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
			der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
			require.NoError(t, err)
			cert, err := x509.ParseCertificate(der)
			require.NoError(t, err)
			encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
			require.NoError(t, err)
			bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})...)
			path := filepath.Join(t.TempDir(), "tls.pem")
			require.NoError(t, os.WriteFile(path, bundle, 0600))
			if invalid {
				path += ".missing"
			}
			secret := make([]byte, 32)
			_, err = rand.Read(secret)
			require.NoError(t, err)
			nodeName := "boot-tls-proof"
			cfg := boot.NewConfig(boot.WithSection("cluster", map[string]any{
				"enabled": true, ClusterNodeName: nodeName, "raft.enabled": false,
				"membership.bind_addr": "127.0.0.1", "membership.bind_port": 0,
				ClusterMembershipSecret: base64.StdEncoding.EncodeToString(secret),
				"internode.bind_addr":   "127.0.0.1", "internode.auto_port": true,
				"internode.identity_key":                  base64.RawStdEncoding.EncodeToString(key),
				"internode.trusted_peer_keys." + nodeName: base64.RawStdEncoding.EncodeToString(pub),
				"internode.tls.enabled":                   true, "internode.tls.cert_file": path,
				"internode.tls.key_file": path, "internode.tls.ca_file": path,
			}))
			base, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx := ctxapi.WithAppContext(base, ctxapi.NewAppContext())
			ctx = boot.WithConfig(ctx, cfg)
			ctx = logsapi.WithLogger(ctx, zap.NewNop())
			ctx = event.WithBus(ctx, eventbus.NewBus())
			ctx = payloadapi.WithTranscoder(ctx, payload.NewTranscoder())
			node := relay.NewNode(nodeName)
			ctx = relayapi.WithNode(ctx, node)
			ctx = relayapi.WithRouter(ctx, relay.NewRouter(node, nil))
			collector := metrics.NewCollector(metricscfg.Config{})
			defer collector.Close()
			ctx = metricsapi.WithCollector(ctx, collector)
			component := Cluster()
			ctx, err = component.Load(ctx)
			require.NoError(t, err)
			defer component.(boot.Stopper).Stop(ctx)
			err = component.(boot.Starter).Start(ctx)
			if invalid {
				require.Error(t, err)
				require.Empty(t, clusterapi.GetMembership(ctx).LocalNode().Meta[internode.MetadataPort])
				return
			}
			require.NoError(t, err)
			info := clusterapi.GetMembership(ctx).LocalNode()
			endpoint := net.JoinHostPort("127.0.0.1", info.Meta[internode.MetadataPort])
			pair, err := tls.X509KeyPair(bundle, bundle)
			require.NoError(t, err)
			roots := x509.NewCertPool()
			roots.AddCert(cert)
			dialer := &net.Dialer{Timeout: time.Second}
			connection, err := tls.DialWithDialer(dialer, "tcp", endpoint, &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})
			require.NoError(t, err)
			require.True(t, connection.ConnectionState().HandshakeComplete)
			require.NotEmpty(t, connection.ConnectionState().VerifiedChains)
			require.NoError(t, connection.NetConn().Close())
			// Cancellation must release admission before Loader reaches Stop;
			// other components may still be draining user processes.
			cancel()
			require.Eventually(t, func() bool {
				probe, err := net.DialTimeout("tcp", endpoint, 50*time.Millisecond)
				if err != nil {
					return true
				}
				_ = probe.Close()
				return false
			}, time.Second, 5*time.Millisecond)
			var stops sync.WaitGroup
			for range 8 {
				stops.Go(func() { _ = component.(boot.Stopper).Stop(ctx) })
			}
			stops.Wait()
			require.NoError(t, component.(boot.Stopper).Stop(ctx))
			listener, err := net.Listen("tcp", endpoint)
			require.NoError(t, err)
			require.NoError(t, listener.Close())
		})
	}
}
