package env

/*
import (
	"context"
	"fmt"
	"path"

	"github.com/ponyruntime/pony/api/vault"
	"go.uber.org/zap"
)

// VaultStorage implements envstorage.Storage interface using a vault backend
type VaultStorage struct {
	client vault.Client
	prefix string
	log    *zap.Logger
}

// NewVaultStorage creates a new vault-based storage
func NewVaultStorage(client vault.Client, prefix string, log *zap.Logger) *VaultStorage {
	return &VaultStorage{
		client: client,
		prefix: prefix,
		log:    log.With(zap.String("component", "vaultstorage"), zap.String("prefix", prefix)),
	}
}

// Get retrieves a value from storage
func (s *VaultStorage) Get(ctx context.Context, key string) (string, bool) {
	path := s.getPath(key)
	secret, err := s.client.Get(ctx, path)
	if err != nil {
		s.log.Debug("failed to get value from vault",
			zap.String("key", key),
			zap.String("path", path),
			zap.Error(err))
		return "", false
	}

	value, ok := secret.Data["value"].(string)
	if !ok {
		s.log.Warn("invalid value type in vault",
			zap.String("key", key),
			zap.String("path", path))
		return "", false
	}

	return value, true
}

// Set stores a value in storage
func (s *VaultStorage) Set(ctx context.Context, key, value string) error {
	path := s.getPath(key)
	data := map[string]interface{}{
		"value": value,
	}

	if err := s.client.Put(ctx, path, data); err != nil {
		return fmt.Errorf("failed to store value in vault: %w", err)
	}

	return nil
}

// Delete removes a value from storage
func (s *VaultStorage) Delete(ctx context.Context, key string) error {
	path := s.getPath(key)
	if err := s.client.Delete(ctx, path); err != nil {
		return fmt.Errorf("failed to delete value from vault: %w", err)
	}

	return nil
}

// getPath returns the full vault path for a given key
func (s *VaultStorage) getPath(key string) string {
	return path.Join(s.prefix, key)
}
*/
