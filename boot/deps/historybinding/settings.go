// SPDX-License-Identifier: MPL-2.0

package historybinding

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	moduleconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/system/registry/history/remote"
)

// DefaultTimeout is the History request timeout.
const DefaultTimeout = 15 * time.Second

// Settings select a remote History. Empty values use defaults from the Wippy
// Hub URL, the saved credential, and the project.
type Settings struct {
	RegistryID      string
	Organization    string
	TenantID        string
	EnvironmentID   string
	Endpoint        string
	TokenFile       string
	CAFile          string
	ServerName      string
	CertFile        string
	KeyFile         string
	Timeout         time.Duration
	MaxMessageBytes int
}

// Resolve returns the connection for settings and the Hub URL of the
// credential.
func (s Settings) Resolve(ctx context.Context, projectDir string) (remote.DialConfig, string, error) {
	dial := s.dialConfig(&historyv1.RegistryKey{TenantId: s.TenantID, EnvironmentId: s.EnvironmentID, RegistryId: s.RegistryID})
	if dial.Key.RegistryId == "" {
		return dial, "", errors.New("history_registry_id is required")
	}
	if dial.Timeout <= 0 {
		return dial, "", errors.New("history timeout must be positive")
	}
	organization := s.Organization
	if dial.Key.TenantId != "" && organization != "" {
		return dial, "", errors.New("set history_organization or history_tenant_id, not both")
	}
	store := bootauth.NewStore(bootauth.NewConfig(projectDir))
	registryURL := store.DefaultRegistry()
	if dial.Endpoint == "" || dial.Key.EnvironmentId == "" {
		endpoint, environment, err := registryDefaults(registryURL)
		if err != nil {
			return dial, "", err
		}
		if dial.Endpoint == "" {
			dial.Endpoint = endpoint
		}
		if dial.Key.EnvironmentId == "" {
			dial.Key.EnvironmentId = environment
		}
	}
	if dial.TokenFile != "" && dial.Key.TenantId != "" {
		return dial, registryURL, nil
	}
	if dial.TokenFile != "" {
		data, err := os.ReadFile(dial.TokenFile)
		if err != nil {
			return dial, "", fmt.Errorf("read history token: %w", err)
		}
		dial.Token = strings.TrimSpace(string(data))
		if dial.Token == "" {
			return dial, "", errors.New("history token file is empty")
		}
		dial.TokenFile = ""
	} else {
		credential, err := store.Get(registryURL)
		if err != nil {
			return dial, "", errors.New("history authentication is required: set WIPPY_TOKEN or run wippy auth login")
		}
		dial.Token = credential.Token
		registryURL = credential.Registry
	}
	if dial.Key.TenantId != "" {
		return dial, registryURL, nil
	}
	if organization == "" {
		if _, err := os.Stat(filepath.Join(projectDir, moduleconfig.DefaultConfigFile)); !errors.Is(err, os.ErrNotExist) {
			project, err := moduleconfig.Load(projectDir)
			if err != nil {
				return dial, "", fmt.Errorf("read history organization from project: %w", err)
			}
			organization = project.Organization
		}
	}
	client, err := bootauth.NewClient(registryURL)
	if err != nil {
		return dial, "", err
	}
	defer client.Close()
	lookupCtx, cancel := context.WithTimeout(ctx, dial.Timeout)
	defer cancel()
	membership, err := client.Validate(lookupCtx, dial.Token)
	if err != nil {
		return dial, "", fmt.Errorf("resolve history organization: %w", err)
	}
	dial.Key.TenantId, err = organizationID(membership.Orgs, organization)
	return dial, registryURL, err
}

func (s Settings) dialConfig(key *historyv1.RegistryKey) remote.DialConfig {
	return remote.DialConfig{
		Config:     remote.Config{Key: key, Timeout: s.Timeout, MaxMessageBytes: s.MaxMessageBytes},
		Endpoint:   s.Endpoint,
		TokenFile:  s.TokenFile,
		CAFile:     s.CAFile,
		ServerName: s.ServerName,
		CertFile:   s.CertFile,
		KeyFile:    s.KeyFile,
	}
}

func organizationID(organizations []bootauth.OrgInfo, name string) (string, error) {
	if len(organizations) == 0 {
		return "", errors.New("no organizations are available for this Wippy token")
	}
	if name == "" && len(organizations) != 1 {
		return "", errors.New("multiple organizations are available; set registry.history_organization to the organization name")
	}
	for _, organization := range organizations {
		if name != "" && organization.Name != name {
			continue
		}
		id, err := uuid.Parse(organization.ID)
		if err != nil || id == uuid.Nil {
			return "", errors.New("the registry returned an invalid organization identity")
		}
		return id.String(), nil
	}
	return "", fmt.Errorf("organization %q is not available for this Wippy token", name)
}

func registryDefaults(registry string) (string, string, error) {
	registryURL, err := url.Parse(registry)
	if err != nil || registryURL.Scheme != "https" || registryURL.User != nil || registryURL.RawQuery != "" || registryURL.ForceQuery || registryURL.Fragment != "" || (registryURL.Path != "" && registryURL.Path != "/") {
		return "", "", errors.New("cannot derive History settings: use an HTTPS Hub URL or set history_endpoint and history_environment_id")
	}
	host := strings.ToLower(registryURL.Hostname())
	domain, ok := strings.CutPrefix(host, "hub.")
	environment, _, hasDomain := strings.Cut(domain, ".")
	if !ok || !hasDomain || strings.HasSuffix(registryURL.Host, ":") {
		return "", "", errors.New("cannot derive History settings: Hub host must start with hub. and include a domain")
	}
	for label := range strings.SplitSeq(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", "", errors.New("cannot derive History settings: invalid Hub domain")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", "", errors.New("cannot derive History settings: invalid Hub domain")
			}
		}
	}
	port := registryURL.Port()
	if port == "" {
		port = "443"
	} else if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return "", "", errors.New("cannot derive History settings: invalid Hub port")
	}
	return net.JoinHostPort("history."+domain, port), environment, nil
}
