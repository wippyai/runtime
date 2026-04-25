// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/auth"
	"gopkg.in/yaml.v3"
)

// refreshSkew is the safety margin applied when checking JWT expiry: we
// refresh the token if it expires within this window so a /resolve→/download
// pair doesn't straddle the boundary.
const refreshSkew = 30 * time.Second

// StoredCredential is the on-disk representation of a credential.
type StoredCredential struct {
	ExpiresAt time.Time `yaml:"expires_at,omitempty"`
	CreatedAt time.Time `yaml:"created_at,omitempty"`
	Token     string    `yaml:"token"`
	Registry  string    `yaml:"registry"`
	UserID    string    `yaml:"user_id,omitempty"`
	Username  string    `yaml:"username,omitempty"`
	Scope     string    `yaml:"scope,omitempty"`
	// SSHKeyPath records the SSH private key used to mint a short-lived JWT.
	// Set when the credential came from 'wippy auth login --ssh' so the store
	// can transparently refresh the token on expiry instead of forcing the
	// user to re-authenticate every hour.
	SSHKeyPath string   `yaml:"ssh_key_path,omitempty"`
	Orgs       []string `yaml:"orgs,omitempty"`
}

// ToCredential converts to the api/auth.Credential type.
func (s *StoredCredential) ToCredential() *auth.Credential {
	return &auth.Credential{
		Token:     s.Token,
		Registry:  s.Registry,
		UserID:    s.UserID,
		Username:  s.Username,
		Scope:     auth.Scope(s.Scope),
		Orgs:      s.Orgs,
		ExpiresAt: s.ExpiresAt,
	}
}

// FromCredential creates a StoredCredential from api/auth.Credential.
func FromCredential(c *auth.Credential) *StoredCredential {
	return &StoredCredential{
		Token:     c.Token,
		Registry:  c.Registry,
		UserID:    c.UserID,
		Username:  c.Username,
		Scope:     string(c.Scope),
		Orgs:      c.Orgs,
		ExpiresAt: c.ExpiresAt,
		CreatedAt: time.Now(),
	}
}

// CredentialsFile is the on-disk file structure.
type CredentialsFile struct {
	Credentials map[string]*StoredCredential `yaml:"credentials"`
	Version     string                       `yaml:"version"`
	Default     string                       `yaml:"default,omitempty"`
}

// Store manages credential persistence.
type Store struct {
	config *Config
	mu     sync.RWMutex
}

// NewStore creates a credential store.
func NewStore(cfg *Config) *Store {
	return &Store{config: cfg}
}

// Get returns the credential for the registry.
// Resolution: env → local → global. When a stored credential carries an
// SSH key reference and is expired (or about to expire), Get transparently
// refreshes the token by replaying the SSH challenge handshake.
func (s *Store) Get(registry string) (*auth.Credential, error) {
	if registry == "" {
		registry = s.DefaultRegistry()
	}

	// Environment override — explicit token wins.
	if token := TokenFromEnv(); token != "" {
		return &auth.Credential{
			Token:    token,
			Registry: registry,
		}, nil
	}

	// Environment override — SSH key. Mint a fresh JWT on every call; the
	// token is never persisted because the env-driven flow is for ephemeral
	// runners that have no writable home directory anyway.
	if keyPath := SSHKeyFromEnv(); keyPath != "" {
		cred, err := s.refreshFromSSH(context.Background(), registry, keyPath)
		if err == nil {
			return cred, nil
		}
		// Fall through to disk-based credentials so a misconfigured env var
		// doesn't lock the user out when valid credentials still exist.
	}

	s.mu.RLock()
	cred, path, stored := s.loadStoredCredential(registry)
	s.mu.RUnlock()

	if cred == nil {
		return nil, auth.ErrNotAuthenticated
	}

	if needsRefresh(stored) {
		refreshed, err := s.refreshFromStored(context.Background(), registry, path, stored)
		if err == nil {
			return refreshed, nil
		}
		if !cred.IsExpired() {
			// Refresh failed but the cached token is still technically valid —
			// hand it out and let the caller decide if the eventual 401 is fatal.
			return cred, nil
		}
		return nil, fmt.Errorf("refresh ssh credential for %s: %w", registry, err)
	}
	return cred, nil
}

// loadStoredCredential walks local→global and returns the first match
// alongside the file path it was loaded from (so callers can write back).
func (s *Store) loadStoredCredential(registry string) (*auth.Credential, string, *StoredCredential) {
	for _, path := range []string{s.config.LocalPath(), s.config.GlobalPath()} {
		file, err := s.loadFile(path)
		if err != nil || file == nil {
			continue
		}
		stored, ok := file.Credentials[registry]
		if !ok || stored == nil {
			continue
		}
		return stored.ToCredential(), path, stored
	}
	return nil, "", nil
}

func needsRefresh(stored *StoredCredential) bool {
	if stored == nil || stored.SSHKeyPath == "" || stored.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(refreshSkew).After(stored.ExpiresAt)
}

func (s *Store) refreshFromStored(ctx context.Context, registry, path string, stored *StoredCredential) (*auth.Credential, error) {
	signer, err := LoadSSHSigner(stored.SSHKeyPath, nil)
	if err != nil {
		return nil, err
	}
	result, err := ExchangeSSHForToken(ctx, registry, signer)
	if err != nil {
		return nil, err
	}

	cred := &auth.Credential{
		Token:     result.Token,
		Registry:  registry,
		UserID:    stored.UserID,
		Username:  stored.Username,
		Scope:     auth.Scope(stored.Scope),
		Orgs:      stored.Orgs,
		ExpiresAt: result.ExpiresAt,
	}

	// Persist refresh so concurrent commands don't all redial.
	updated := FromCredential(cred)
	updated.SSHKeyPath = stored.SSHKeyPath
	updated.CreatedAt = stored.CreatedAt
	if err := s.writeStored(path, updated); err != nil {
		return cred, fmt.Errorf("persist refreshed token: %w", err)
	}
	return cred, nil
}

// refreshFromSSH performs an unstored SSH→JWT exchange driven entirely by
// env vars. Used when WIPPY_SSH_KEY is set.
func (s *Store) refreshFromSSH(ctx context.Context, registry, keyPath string) (*auth.Credential, error) {
	signer, err := LoadSSHSigner(keyPath, nil)
	if err != nil {
		return nil, err
	}
	result, err := ExchangeSSHForToken(ctx, registry, signer)
	if err != nil {
		return nil, err
	}
	return &auth.Credential{
		Token:     result.Token,
		Registry:  registry,
		ExpiresAt: result.ExpiresAt,
	}, nil
}

// Set stores a credential.
func (s *Store) Set(cred *auth.Credential, global bool) error {
	return s.set(cred, "", global)
}

// SetWithSSHKey stores a credential together with the SSH private key path
// that produced it. The path is persisted so future commands can refresh
// the short-lived JWT transparently when it expires.
func (s *Store) SetWithSSHKey(cred *auth.Credential, sshKeyPath string, global bool) error {
	return s.set(cred, sshKeyPath, global)
}

func (s *Store) set(cred *auth.Credential, sshKeyPath string, global bool) error {
	path := s.config.LocalPath()
	if global {
		path = s.config.GlobalPath()
	}

	if path == "" {
		return fmt.Errorf("credentials path not configured")
	}

	stored := FromCredential(cred)
	stored.SSHKeyPath = sshKeyPath
	return s.writeStored(path, stored)
}

// Remove removes a credential.
func (s *Store) Remove(registry string, global bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.config.LocalPath()
	if global {
		path = s.config.GlobalPath()
	}

	file, err := s.loadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load credentials: %w", err)
	}

	if file == nil || file.Credentials == nil {
		return nil
	}

	delete(file.Credentials, registry)

	if file.Default == registry {
		file.Default = ""
		for r := range file.Credentials {
			file.Default = r
			break
		}
	}

	return s.saveFile(path, file)
}

// DefaultRegistry returns the default registry URL.
func (s *Store) DefaultRegistry() string {
	if reg := RegistryFromEnv(); reg != "" {
		return reg
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if file, _ := s.loadFile(s.config.GlobalPath()); file != nil && file.Default != "" {
		return file.Default
	}

	return DefaultRegistry
}

// writeStored merges a single StoredCredential into the credentials file at
// path, taking the lock so concurrent refreshes don't corrupt the YAML.
func (s *Store) writeStored(path string, stored *StoredCredential) error {
	if path == "" {
		return fmt.Errorf("credentials path not configured")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.loadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load credentials: %w", err)
	}
	if file == nil {
		file = &CredentialsFile{
			Version:     "1.0",
			Credentials: make(map[string]*StoredCredential),
		}
	}
	if file.Credentials == nil {
		file.Credentials = make(map[string]*StoredCredential)
	}

	file.Credentials[stored.Registry] = stored
	if file.Default == "" {
		file.Default = stored.Registry
	}
	return s.saveFile(path, file)
}

func (s *Store) loadFile(path string) (*CredentialsFile, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var file CredentialsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	return &file, nil
}

func (s *Store) saveFile(path string, file *CredentialsFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("serialize credentials: %w", err)
	}

	// Atomic write: write to temp file then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("save credentials: %w", err)
	}

	return nil
}
