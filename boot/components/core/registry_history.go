// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/boot"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/historybinding"
	"github.com/wippyai/runtime/system/registry/history/composite"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historynil "github.com/wippyai/runtime/system/registry/history/nil"
	"github.com/wippyai/runtime/system/registry/history/postgres"
	"github.com/wippyai/runtime/system/registry/history/remote"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
)

// openHistory opens the history that the project binding selects, or else the
// configured history. A durable history is wrapped in a composite history, so
// a running registry can switch it.
func openHistory(ctx context.Context, cfg boot.Config, logger *zap.Logger) (regapi.History, io.Closer, error) {
	if cfg == nil {
		return composite.New(historymem.New()), nil, nil
	}
	registryCfg := cfg.Sub(RegistryName)
	if !registryCfg.GetBool(RegistryEnableHistory, true) {
		return historynil.New(), nil, nil
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve project directory: %w", err)
	}
	settings := historySettings(registryCfg)
	bound, binding, err := historybinding.Open(ctx, projectDir, settings)
	if err != nil {
		return nil, nil, fmt.Errorf("open bound registry history: %w", err)
	}
	if bound != nil {
		logger.Info("using bound remote registry history", zap.String("registry_id", binding.RegistryID))
		history := composite.New(bound)
		return history, history, nil
	}

	historyType := registryCfg.GetString(RegistryHistoryType, "")
	if historyType == "" {
		historyType = "memory"
		if settings.RegistryID != "" {
			historyType = "remote"
		}
	}
	var driver composite.Driver
	switch historyType {
	case "sqlite":
		historyPath := registryCfg.GetString(RegistryHistoryPath, ".wippy/registry.db")
		absPath, err := filepath.Abs(historyPath)
		if err != nil {
			return nil, nil, NewHistoryPathError(err)
		}
		sqliteHist, err := sqlite.NewSQLite(absPath, logger.Named("history"))
		if err != nil {
			return nil, nil, NewSQLiteHistoryError(err)
		}
		driver = sqliteHist
	case "postgres":
		postgresHist, err := postgres.NewPostgres(registryCfg.GetString(RegistryHistoryDSN, ""), registryCfg.GetString(RegistryHistorySchema, ""), logger.Named("history"))
		if err != nil {
			return nil, nil, NewPostgresHistoryError(err)
		}
		driver = postgresHist
	case "remote":
		dial, _, err := settings.Resolve(ctx, projectDir)
		if err != nil {
			return nil, nil, err
		}
		remoteHist, err := remote.Dial(ctx, dial)
		if err != nil {
			return nil, nil, err
		}
		driver = remoteHist
	case "nil":
		return historynil.New(), nil, nil
	case "memory":
		driver = historymem.New()
	default:
		logger.Warn("unknown history type, defaulting to memory", zap.String("type", historyType))
		driver = historymem.New()
	}
	history := composite.New(driver)
	return history, history, nil
}

func historySettings(cfg boot.Config) historybinding.Settings {
	return historybinding.Settings{
		RegistryID:      cfg.GetString("history_registry_id", ""),
		Organization:    cfg.GetString("history_organization", ""),
		TenantID:        cfg.GetString("history_tenant_id", ""),
		EnvironmentID:   cfg.GetString("history_environment_id", ""),
		Endpoint:        cfg.GetString("history_endpoint", ""),
		TokenFile:       cfg.GetString("history_token_file", ""),
		CAFile:          cfg.GetString("history_ca_file", ""),
		ServerName:      cfg.GetString("history_server_name", ""),
		CertFile:        cfg.GetString("history_cert_file", ""),
		KeyFile:         cfg.GetString("history_key_file", ""),
		Timeout:         cfg.GetDuration("history_timeout", historybinding.DefaultTimeout),
		MaxMessageBytes: cfg.GetInt("history_max_message_bytes", remote.MaxMessageBytes),
	}
}
