// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func managerTestTLSConfigs(t *testing.T) (ManagerTLSConfig, ManagerTLSConfig) {
	t.Helper()
	rootPublic, rootPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	root := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, root, root, rootPublic, rootPrivate)
	require.NoError(t, err)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	leaf := func(name string, serial int64) ManagerTLSConfig {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		certificate := &x509.Certificate{
			SerialNumber: big.NewInt(serial), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
			KeyUsage:    x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, certificate, root, public, rootPrivate)
		require.NoError(t, err)
		key, err := x509.MarshalPKCS8PrivateKey(private)
		require.NoError(t, err)
		cfg := ManagerTLSConfig{Enabled: true, CAFile: caPath, CertFile: filepath.Join(dir, name+".pem"), KeyFile: filepath.Join(dir, name+".key")}
		require.NoError(t, os.WriteFile(cfg.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
		require.NoError(t, os.WriteFile(cfg.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
		return cfg
	}
	return leaf("one", 2), leaf("two", 3)
}

func TestManagerTLSSetupFailureDoesNotFallBackToPlaintext(t *testing.T) {
	config := insecureManagerConfig()
	config.LocalNodeID = "test"
	config.BindAddr = "127.0.0.1"
	config.Logger = zap.NewNop()
	config.TLS = ManagerTLSConfig{Enabled: true, CertFile: filepath.Join(t.TempDir(), "missing.pem")}
	m := NewConnectionManager(config, nil).(*manager)
	require.True(t, m.ProtectsPayloads())
	require.Error(t, m.Start(t.Context(), func(cluster.NodeID, []byte) { t.Error("unexpected delivery") }))
	require.Nil(t, m.listener)
	require.NoError(t, m.Stop())
}
