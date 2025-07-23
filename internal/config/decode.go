package config

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// DecodeAndInitConfig decodes the configuration and initializes defaults
func DecodeAndInitConfig[T any](dtt payload.Transcoder, entry registry.Entry) (*T, error) {
	// Get logger from context if available
	logger := logs.GetLogger(context.Background())

	if entry.Data == nil {
		logger.Error("DecodeAndInitConfig - entry data is nil",
			zap.String("entry_id", entry.ID.String()),
			zap.String("entry_kind", entry.Kind),
		)
		return nil, fmt.Errorf("configuration data is required")
	}

	// Info: Log payload details
	logger.Info("DecodeAndInitConfig - starting decode",
		zap.String("entry_id", entry.ID.String()),
		zap.String("entry_kind", entry.Kind),
		zap.String("payload_format", string(entry.Data.Format())),
		zap.String("target_type", fmt.Sprintf("%T", new(T))),
		zap.String("payload_data_type", fmt.Sprintf("%T", entry.Data.Data())),
		zap.String("dtt", fmt.Sprintf("%T", dtt)),
	)

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		logger.Error("DecodeAndInitConfig - unmarshal failed",
			zap.String("entry_id", entry.ID.String()),
			zap.String("entry_kind", entry.Kind),
			zap.String("payload_format", string(entry.Data.Format())),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	logger.Info("DecodeAndInitConfig - unmarshal successful",
		zap.String("entry_id", entry.ID.String()),
		zap.String("entry_kind", entry.Kind),
	)

	// Initialize defaults if the config implements InitDefaults
	if defaulter, ok := interface{}(cfg).(interface{ InitDefaults() }); ok {
		logger.Info("DecodeAndInitConfig - initializing defaults",
			zap.String("entry_id", entry.ID.String()),
			zap.String("entry_kind", entry.Kind),
		)
		defaulter.InitDefaults()
	}

	// Validate if the config implements Validate
	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		logger.Info("DecodeAndInitConfig - validating config",
			zap.String("entry_id", entry.ID.String()),
			zap.String("entry_kind", entry.Kind),
		)
		if err := validator.Validate(); err != nil {
			logger.Error("DecodeAndInitConfig - validation failed",
				zap.String("entry_id", entry.ID.String()),
				zap.String("entry_kind", entry.Kind),
				zap.Error(err),
			)
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	logger.Info("DecodeAndInitConfig - decode completed successfully",
		zap.String("entry_id", entry.ID.String()),
		zap.String("entry_kind", entry.Kind),
	)

	return cfg, nil
}
