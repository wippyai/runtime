// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/auth"
	"gopkg.in/yaml.v3"
)

// StoredCredential is the on-disk representation of a credential.
type StoredCredential struct {
	ExpiresAt time.Time `yaml:"expires_at,omitempty"`
	CreatedAt time.Time `yaml:"created_at,omitempty"`
	Token     string    `yaml:"token"`
	Registry  string    `yaml:"registry"`
	UserID    string    `yaml:"user_id,omitempty"`
	Username  string    `yaml:"username,omitempty"`
	Scope     string    `yaml:"scope,omitempty"`
	Orgs      []string  `yaml:"orgs,omitempty"`
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
// Resolution: env → local → global.
func (s *Store) Get(registry string) (*auth.Credential, error) {
	if registry == "" {
		registry = s.DefaultRegistry()
	}

	// Environment override
	if token := TokenFromEnv(); token != "" {
		return &auth.Credential{
			Token:    token,
			Registry: registry,
		}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Local credentials
	if cred := s.loadCredential(s.config.LocalPath(), registry); cred != nil {
		return cred, nil
	}

	// Global credentials
	if cred := s.loadCredential(s.config.GlobalPath(), registry); cred != nil {
		return cred, nil
	}

	return nil, auth.ErrNotAuthenticated
}

// Set stores a credential.
func (s *Store) Set(cred *auth.Credential, global bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.config.LocalPath()
	if global {
		path = s.config.GlobalPath()
	}

	if path == "" {
		return fmt.Errorf("credentials path not configured")
	}

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
		// loadFile may return a non-nil file with a nil map when the on-disk
		// YAML omits the `credentials` key (e.g. a hand-edited file or a
		// partially migrated v0 file). Lazy-init so Set() doesn't panic.
		file.Credentials = make(map[string]*StoredCredential)
	}

	file.Credentials[cred.Registry] = FromCredential(cred)

	if file.Default == "" {
		file.Default = cred.Registry
	}

	return s.saveFile(path, file)
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

func (s *Store) loadCredential(path, registry string) *auth.Credential {
	file, err := s.loadFile(path)
	if err != nil || file == nil {
		return nil
	}

	if stored := file.Credentials[registry]; stored != nil {
		return stored.ToCredential()
	}

	return nil
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
