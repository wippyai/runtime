// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/version"
	"golang.org/x/crypto/ssh"
)

// EnvSSHKey lets the user point publish/install/run flows at a private SSH
// key without first running 'wippy auth login'. Useful for ephemeral CI
// runners that already have a deploy key on disk.
const EnvSSHKey = "WIPPY_SSH_KEY"

// SSHAuthEndpoint is the runtime CLI's view of the hub's SSH challenge
// endpoint. The hub forwards the result to the auth-service and returns a
// short-lived JWT (1h TTL).
const SSHAuthEndpoint = "/api/v1/ssh/auth"

// defaultSSHKeyCandidates lists the conventional locations that
// 'wippy auth login --ssh' probes when the user does not pass --key.
// Order matters: ed25519 first (modern default), then ecdsa, then rsa.
var defaultSSHKeyCandidates = []string{
	"id_ed25519",
	"id_ecdsa",
	"id_rsa",
}

// SSHKeyFromEnv returns the value of WIPPY_SSH_KEY, expanding ~ to the
// user's home directory.
func SSHKeyFromEnv() string {
	return expandHome(os.Getenv(EnvSSHKey))
}

// FindDefaultSSHKey returns the first existing key under ~/.ssh that the CLI
// will probe automatically. The empty string means "no usable key found".
func FindDefaultSSHKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range defaultSSHKeyCandidates {
		path := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// PassphrasePrompter resolves a passphrase for an encrypted private key.
// Implementations typically read from the controlling terminal.
type PassphrasePrompter func(keyPath string) ([]byte, error)

// SSHSigner pairs a parsed private key with its public-key fingerprint.
type SSHSigner struct {
	signer      ssh.Signer
	fingerprint string
	keyPath     string
}

// LoadSSHSigner reads and decodes an SSH private key. Encrypted keys trigger
// the prompter (which is allowed to be nil — in that case encrypted keys
// fail with an explanatory error). The returned signer can sign arbitrary
// challenges for the hub's /api/v1/ssh/auth handshake.
func LoadSSHSigner(keyPath string, prompter PassphrasePrompter) (*SSHSigner, error) {
	keyPath = expandHome(keyPath)
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key %q: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err == nil {
		return newSSHSigner(signer, keyPath), nil
	}

	var passErr *ssh.PassphraseMissingError
	if !errors.As(err, &passErr) {
		return nil, fmt.Errorf("parse ssh key %q: %w", keyPath, err)
	}

	if prompter == nil {
		return nil, fmt.Errorf("ssh key %q is encrypted; pass a passphrase prompter", keyPath)
	}

	pass, err := prompter(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read passphrase for %q: %w", keyPath, err)
	}

	signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, pass)
	if err != nil {
		return nil, fmt.Errorf("parse encrypted ssh key %q: %w", keyPath, err)
	}
	return newSSHSigner(signer, keyPath), nil
}

func newSSHSigner(signer ssh.Signer, keyPath string) *SSHSigner {
	return &SSHSigner{
		signer:      signer,
		fingerprint: sshFingerprint(signer.PublicKey()),
		keyPath:     keyPath,
	}
}

// Fingerprint returns the SHA256 fingerprint of the public key in the
// canonical openssh form ("SHA256:<base64>"). The hub indexes registered
// keys by this value.
func (s *SSHSigner) Fingerprint() string { return s.fingerprint }

// KeyPath returns the absolute path the signer was loaded from.
func (s *SSHSigner) KeyPath() string { return s.keyPath }

// SSHAuthResult is the runtime CLI's view of /api/v1/ssh/auth's response.
type SSHAuthResult struct {
	ExpiresAt time.Time
	Token     string
}

// ExchangeSSHForToken performs a challenge-response handshake against the
// hub's SSH auth endpoint and returns the resulting Bearer JWT.
//
// The challenge is a random 32-byte payload prefixed with a length-tagged
// "wippy-ssh-auth\0" domain separator and the current Unix timestamp, so
// the auth-service can reject stale challenges if it ever wants to (today
// it doesn't, but the format leaves room).
func ExchangeSSHForToken(ctx context.Context, registryURL string, signer *SSHSigner) (*SSHAuthResult, error) {
	if signer == nil {
		return nil, errors.New("ssh signer is nil")
	}
	if err := requireHTTPS(registryURL); err != nil {
		return nil, err
	}

	challenge, err := buildChallenge()
	if err != nil {
		return nil, fmt.Errorf("build challenge: %w", err)
	}

	sig, err := signer.signer.Sign(rand.Reader, challenge)
	if err != nil {
		return nil, fmt.Errorf("sign challenge: %w", err)
	}

	body := map[string]string{
		"fingerprint": signer.fingerprint,
		"signature":   base64.StdEncoding.EncodeToString(ssh.Marshal(sig)),
		"challenge":   base64.StdEncoding.EncodeToString(challenge),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimSuffix(registryURL, "/") + SSHAuthEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wippy-cli/"+version.Version)

	resp, err := sshClientForRequest().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ssh auth request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read ssh auth response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("ssh auth failed (status %d): %s", resp.StatusCode, msg)
	}

	var parsed struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode ssh auth response: %w", err)
	}
	if parsed.Token == "" {
		return nil, errors.New("ssh auth response missing token")
	}

	expiresAt := time.Time{}
	if parsed.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return &SSHAuthResult{Token: parsed.Token, ExpiresAt: expiresAt}, nil
}

func sshFingerprint(pub ssh.PublicKey) string {
	sum := sha256.Sum256(pub.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// buildChallenge produces 32 bytes of random material framed with a domain
// separator and a Unix timestamp. The hub treats it as opaque bytes today;
// the framing keeps the door open to add freshness/replay checks server-side
// without breaking older clients.
func buildChallenge() ([]byte, error) {
	const tag = "wippy-ssh-auth\x00"
	now := time.Now().Unix()

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	buf := make([]byte, 0, len(tag)+8+len(nonce))
	buf = append(buf, tag...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(now))
	buf = append(buf, nonce...)
	return buf, nil
}

func requireHTTPS(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid registry URL %q: %w", rawURL, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	if u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1") {
		return nil
	}
	return fmt.Errorf("registry %q must use HTTPS for ssh auth", rawURL)
}

func sshHTTPClient() *http.Client {
	return &http.Client{
		Timeout: clientTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

// sshClientForRequest is the seam tests swap so they can talk to an
// httptest.NewTLSServer (whose self-signed cert is only trusted by its
// associated client). Production code keeps using sshHTTPClient.
var sshClientForRequest = sshHTTPClient

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
