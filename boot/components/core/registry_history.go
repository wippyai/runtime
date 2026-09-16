package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/boot"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	moduleconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/system/registry/history/remote"
)

func historyConnectionConfig(ctx context.Context, cfg boot.Config) (remote.DialConfig, error) {
	dial := remote.DialConfig{
		Config: remote.Config{
			Key: &historyv1.RegistryKey{
				TenantId:      cfg.GetString("history_tenant_id", ""),
				EnvironmentId: cfg.GetString("history_environment_id", ""),
				RegistryId:    cfg.GetString("history_registry_id", ""),
			},
			ReplicaID:       cfg.GetString("history_replica_id", ""),
			Timeout:         cfg.GetDuration("history_timeout", 15*time.Second),
			PollInterval:    cfg.GetDuration("history_poll_interval", 100*time.Millisecond),
			MaxMessageBytes: cfg.GetInt("history_max_message_bytes", remote.MaxMessageBytes),
		},
		Endpoint:   cfg.GetString("history_endpoint", ""),
		TokenFile:  cfg.GetString("history_token_file", ""),
		CAFile:     cfg.GetString("history_ca_file", ""),
		ServerName: cfg.GetString("history_server_name", ""),
		CertFile:   cfg.GetString("history_cert_file", ""),
		KeyFile:    cfg.GetString("history_key_file", ""),
	}
	if dial.Endpoint == "" {
		return dial, nil
	}
	if dial.Timeout <= 0 {
		return dial, errors.New("history timeout must be positive")
	}
	organization := cfg.GetString("history_organization", "")
	if dial.Key.TenantId != "" && organization != "" {
		return dial, errors.New("set history_organization or history_tenant_id, not both")
	}
	if dial.TokenFile != "" && dial.Key.TenantId != "" {
		return dial, nil
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return dial, fmt.Errorf("resolve history credentials: %w", err)
	}
	store := bootauth.NewStore(bootauth.NewConfig(projectDir))
	registryURL := store.DefaultRegistry()
	if dial.TokenFile != "" {
		data, err := os.ReadFile(dial.TokenFile)
		if err != nil {
			return dial, fmt.Errorf("read history token: %w", err)
		}
		dial.Token = strings.TrimSpace(string(data))
		if dial.Token == "" {
			return dial, errors.New("history token file is empty")
		}
		dial.TokenFile = ""
	} else {
		credential, err := store.Get(registryURL)
		if err != nil {
			return dial, errors.New("history authentication is required: set WIPPY_TOKEN or run wippy auth login")
		}
		dial.Token = credential.Token
		registryURL = credential.Registry
	}
	if dial.Key.TenantId != "" {
		return dial, nil
	}
	if organization == "" {
		if _, err := os.Stat(filepath.Join(projectDir, moduleconfig.DefaultConfigFile)); !errors.Is(err, os.ErrNotExist) {
			project, err := moduleconfig.Load(projectDir)
			if err != nil {
				return dial, fmt.Errorf("read history organization from project: %w", err)
			}
			organization = project.Organization
		}
	}
	client, err := bootauth.NewClient(registryURL)
	if err != nil {
		return dial, err
	}
	defer client.Close()
	lookupCtx, cancel := context.WithTimeout(ctx, dial.Timeout)
	defer cancel()
	membership, err := client.Validate(lookupCtx, dial.Token)
	if err != nil {
		return dial, fmt.Errorf("resolve history organization: %w", err)
	}
	dial.Key.TenantId, err = historyOrganizationID(membership.Orgs, organization)
	return dial, err
}

func historyOrganizationID(organizations []bootauth.OrgInfo, name string) (string, error) {
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
