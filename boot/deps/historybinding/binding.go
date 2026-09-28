// SPDX-License-Identifier: MPL-2.0

// Package historybinding stores and applies the selected registry history.
package historybinding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/auth"
	"github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/system/registry/history/composite"
	"github.com/wippyai/runtime/system/registry/history/remote"
	"gopkg.in/yaml.v3"
)

const (
	// FileName is the binding file in the project .wippy directory.
	FileName = "history.yaml"

	BackendLocal  = "local"
	BackendRemote = "remote"
)

// Binding is the selected history. It holds no credentials. A local binding
// can keep a pending remote selection, so a retry resumes the same transfer.
type Binding struct {
	Pending       *Binding `yaml:"pending,omitempty"`
	Backend       string   `yaml:"backend"`
	Registry      string   `yaml:"registry,omitempty"`
	Endpoint      string   `yaml:"endpoint,omitempty"`
	TenantID      string   `yaml:"tenant_id,omitempty"`
	EnvironmentID string   `yaml:"environment_id,omitempty"`
	RegistryID    string   `yaml:"registry_id,omitempty"`
	TransferID    string   `yaml:"transfer_id,omitempty"`
}

var dialRemote = remote.Dial

func path(projectDir string) string {
	return filepath.Join(projectDir, ".wippy", FileName)
}

// Load returns the stored binding, or nil when there is none.
func Load(projectDir string) (*Binding, error) {
	data, err := os.ReadFile(path(projectDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history binding: %w", err)
	}
	var binding Binding
	if err := yaml.Unmarshal(data, &binding); err != nil {
		return nil, fmt.Errorf("read history binding: %w", err)
	}
	if binding.Backend != BackendLocal && binding.Backend != BackendRemote {
		return nil, fmt.Errorf("read history binding: unknown backend %q", binding.Backend)
	}
	if binding.Backend == BackendRemote && !binding.complete() {
		return nil, errors.New("read history binding: remote history identity is incomplete")
	}
	return &binding, nil
}

// Save replaces the stored binding atomically.
func Save(projectDir string, binding *Binding) error {
	data, err := yaml.Marshal(binding)
	if err != nil {
		return fmt.Errorf("write history binding: %w", err)
	}
	target := path(projectDir)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("write history binding: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".history-*.yaml")
	if err != nil {
		return fmt.Errorf("write history binding: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), target)
	}
	if err != nil {
		return fmt.Errorf("write history binding: %w", err)
	}
	return nil
}

func (b *Binding) complete() bool {
	return b.Registry != "" && b.Endpoint != "" && b.TenantID != "" && b.EnvironmentID != "" && b.RegistryID != ""
}

func (b *Binding) key() *historyv1.RegistryKey {
	return &historyv1.RegistryKey{TenantId: b.TenantID, EnvironmentId: b.EnvironmentID, RegistryId: b.RegistryID}
}

func (b *Binding) sameRemote(other *Binding) bool {
	return other != nil && b.Registry == other.Registry && b.Endpoint == other.Endpoint && b.TenantID == other.TenantID &&
		b.EnvironmentID == other.EnvironmentID && b.RegistryID == other.RegistryID
}

// Open connects to the remote history of a remote binding. It returns nil when
// the project has no remote binding. The credential comes from the Wippy
// credential store. settings supply TLS options and limits only.
func Open(ctx context.Context, projectDir string, settings Settings) (*remote.History, *Binding, error) {
	binding, err := Load(projectDir)
	if err != nil || binding == nil || binding.Backend != BackendRemote {
		return nil, nil, err
	}
	credential, err := bootauth.NewStore(bootauth.NewConfig(projectDir)).Get(binding.Registry)
	if err != nil {
		return nil, nil, fmt.Errorf("history authentication is required for %s: set WIPPY_TOKEN or run wippy auth login", binding.Registry)
	}
	settings.Endpoint = binding.Endpoint
	settings.TokenFile = ""
	dial := settings.dialConfig(binding.key())
	dial.Token = credential.Token
	history, err := dialRemote(ctx, dial)
	if err != nil {
		return nil, nil, err
	}
	return history, binding, nil
}

// saveRuntimeCredential stores a token that exists only in this process in the
// project credential store, so a restart can open the remote history.
func saveRuntimeCredential(projectDir, registryURL, token string) error {
	if token == "" || bootauth.RuntimeToken(registryURL) != token {
		return nil
	}
	if err := bootauth.NewStore(bootauth.NewConfig(projectDir)).Set(&auth.Credential{Token: token, Registry: registryURL}, false); err != nil {
		return fmt.Errorf("store history credential: %w", err)
	}
	return nil
}

// UseRemote transfers the active local history and its baseline to the remote
// history that settings select, stores the binding, and switches history to
// it. No registry operation runs during the transfer. When UseRemote fails,
// the local history stays active and unchanged, and a retry resumes the same
// transfer.
func UseRemote(ctx context.Context, history *composite.History, projectDir string, settings Settings) (*Binding, error) {
	current, err := Load(projectDir)
	if err != nil {
		return nil, err
	}
	if current != nil && current.Backend == BackendRemote {
		return nil, fmt.Errorf("registry history already uses remote history %q", current.RegistryID)
	}
	dial, registryURL, err := settings.Resolve(ctx, projectDir)
	if err != nil {
		return nil, err
	}
	target := &Binding{
		Backend:       BackendRemote,
		Registry:      registryURL,
		Endpoint:      dial.Endpoint,
		TenantID:      dial.Key.GetTenantId(),
		EnvironmentID: dial.Key.GetEnvironmentId(),
		RegistryID:    dial.Key.GetRegistryId(),
	}
	if current != nil && target.sameRemote(current.Pending) {
		target.TransferID = current.Pending.TransferID
	} else {
		target.TransferID = uuid.NewString()
	}
	if err := Save(projectDir, &Binding{Backend: BackendLocal, Pending: target}); err != nil {
		return nil, err
	}
	remoteHistory, err := dialRemote(ctx, dial)
	if err != nil {
		return nil, err
	}
	err = history.Switch(func(source composite.Driver, baseline registry.State) (composite.Driver, error) {
		if err := remoteHistory.Transfer(ctx, target.TransferID, source, baseline); err != nil {
			return nil, err
		}
		if err := saveRuntimeCredential(projectDir, registryURL, dial.Token); err != nil {
			return nil, err
		}
		if err := Save(projectDir, target); err != nil {
			return nil, err
		}
		return remoteHistory, nil
	})
	if err != nil {
		_ = remoteHistory.Close()
		return nil, fmt.Errorf("use remote history: %w", err)
	}
	return target, nil
}
