// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// writeUnencryptedED25519Key writes a fresh ed25519 OpenSSH-format private
// key to disk and returns the path plus its expected SHA256 fingerprint.
func writeUnencryptedED25519Key(t *testing.T) (string, string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0600))

	signer, err := ssh.ParsePrivateKey(pem.EncodeToMemory(pemBlock))
	require.NoError(t, err)
	return keyPath, sshFingerprint(signer.PublicKey())
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, home, expandHome("~"))
	assert.Equal(t, filepath.Join(home, ".ssh", "id_rsa"), expandHome("~/.ssh/id_rsa"))
	assert.Equal(t, "/etc/passwd", expandHome("/etc/passwd"))
	assert.Equal(t, "", expandHome(""))
}

func TestRequireHTTPS(t *testing.T) {
	assert.NoError(t, requireHTTPS("https://hub.wippy.ai"))
	assert.NoError(t, requireHTTPS("http://localhost:8080"))
	assert.NoError(t, requireHTTPS("http://127.0.0.1:8080"))
	assert.Error(t, requireHTTPS("http://hub.wippy.ai"))
	assert.Error(t, requireHTTPS("ftp://hub.wippy.ai"))
}

func TestBuildChallenge_FormatAndUniqueness(t *testing.T) {
	first, err := buildChallenge()
	require.NoError(t, err)
	second, err := buildChallenge()
	require.NoError(t, err)

	// 15 bytes "wippy-ssh-auth\0" + 8 bytes timestamp + 32 bytes nonce.
	assert.Equal(t, 15+8+32, len(first))
	assert.True(t, strings.HasPrefix(string(first), "wippy-ssh-auth\x00"))
	assert.NotEqual(t, first, second, "challenge nonce must differ between calls")
}

func TestLoadSSHSigner(t *testing.T) {
	keyPath, fingerprint := writeUnencryptedED25519Key(t)

	signer, err := LoadSSHSigner(keyPath, nil)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, signer.Fingerprint())
	assert.Equal(t, keyPath, signer.KeyPath())
}

func TestLoadSSHSigner_FileMissing(t *testing.T) {
	_, err := LoadSSHSigner(filepath.Join(t.TempDir(), "missing"), nil)
	require.Error(t, err)
}

func TestExchangeSSHForToken_HappyPath(t *testing.T) {
	keyPath, fingerprint := writeUnencryptedED25519Key(t)

	var receivedFingerprint string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != SSHAuthEndpoint {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Fingerprint string `json:"fingerprint"`
			Signature   string `json:"signature"`
			Challenge   string `json:"challenge"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		receivedFingerprint = body.Fingerprint

		// Verify the signature with the registered public key — exactly what
		// the auth-service does on the production path.
		serverSigner, err := parseSignerForTest(keyPath)
		require.NoError(t, err)

		challenge, err := base64.StdEncoding.DecodeString(body.Challenge)
		require.NoError(t, err)
		sigBytes, err := base64.StdEncoding.DecodeString(body.Signature)
		require.NoError(t, err)

		var sig ssh.Signature
		require.NoError(t, ssh.Unmarshal(sigBytes, &sig))
		require.NoError(t, serverSigner.PublicKey().Verify(challenge, &sig))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "issued-jwt",
			"expires_in": 3600,
		})
	}))
	defer srv.Close()

	signer, err := LoadSSHSigner(keyPath, nil)
	require.NoError(t, err)

	res, err := exchangeWithClient(context.Background(), srv.URL, signer, srv.Client())
	require.NoError(t, err)
	assert.Equal(t, "issued-jwt", res.Token)
	assert.WithinDuration(t, time.Now().Add(time.Hour), res.ExpiresAt, 5*time.Second)
	assert.Equal(t, fingerprint, receivedFingerprint)
}

func TestExchangeSSHForToken_Unauthorized(t *testing.T) {
	keyPath, _ := writeUnencryptedED25519Key(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	signer, err := LoadSSHSigner(keyPath, nil)
	require.NoError(t, err)

	_, err = exchangeWithClient(context.Background(), srv.URL, signer, srv.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func parseSignerForTest(path string) (ssh.Signer, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(pemBytes)
}

// exchangeWithClient swaps the ssh-auth HTTP client for the duration of the
// call so tests can talk to httptest.NewTLSServer's in-memory cert.
func exchangeWithClient(ctx context.Context, registryURL string, signer *SSHSigner, client *http.Client) (*SSHAuthResult, error) {
	prev := sshClientForRequest
	sshClientForRequest = func() *http.Client { return client }
	defer func() { sshClientForRequest = prev }()
	return ExchangeSSHForToken(ctx, registryURL, signer)
}
