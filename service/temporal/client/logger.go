package client

import (
	tlog "go.temporal.io/sdk/log"
	"go.uber.org/zap"
)

type zapLogger struct {
	logger *zap.Logger
	skip   int
}

// newZapLogger creates a new Temporal logger that wraps a zap.Logger
func newZapLogger(logger *zap.Logger) tlog.Logger {
	return &zapLogger{
		logger: logger,
		skip:   1,
	}
}

func (z *zapLogger) Debug(msg string, keyvals ...interface{}) {
	if ce := z.logger.Check(zap.DebugLevel, msg); ce != nil {
		ce.Write(z.convertToFields(keyvals)...)
	}
}

func (z *zapLogger) Info(msg string, keyvals ...interface{}) {
	if ce := z.logger.Check(zap.InfoLevel, msg); ce != nil {
		ce.Write(z.convertToFields(keyvals)...)
	}
}

func (z *zapLogger) Warn(msg string, keyvals ...interface{}) {
	if ce := z.logger.Check(zap.WarnLevel, msg); ce != nil {
		ce.Write(z.convertToFields(keyvals)...)
	}
}

func (z *zapLogger) Error(msg string, keyvals ...interface{}) {
	if ce := z.logger.Check(zap.ErrorLevel, msg); ce != nil {
		ce.Write(z.convertToFields(keyvals)...)
	}
}

// WithCallerSkip implements WithSkipCallers interface
func (z *zapLogger) WithCallerSkip(skip int) tlog.Logger {
	return &zapLogger{
		logger: z.logger.WithOptions(zap.AddCallerSkip(skip)),
		skip:   z.skip + skip,
	}
}

// With implements WithLogger interface
func (z *zapLogger) With(keyvals ...interface{}) tlog.Logger {
	return &zapLogger{
		logger: z.logger.With(z.convertToFields(keyvals)...),
		skip:   z.skip,
	}
}

// convertToFields converts key-value pairs to zap.Field slice
func (z *zapLogger) convertToFields(keyvals []interface{}) []zap.Field {
	if len(keyvals) == 0 {
		return nil
	}

	// If odd number of elements, add empty string to make it even
	if len(keyvals)%2 != 0 {
		keyvals = append(keyvals, "")
	}

	fields := make([]zap.Field, 0, len(keyvals)/2)
	for i := 0; i < len(keyvals); i += 2 {
		// Convert key to string, skip if not possible
		key, ok := keyvals[i].(string)
		if !ok {
			continue
		}

		// Convert value based on type
		value := keyvals[i+1]
		switch v := value.(type) {
		case string:
			fields = append(fields, zap.String(key, v))
		case int:
			fields = append(fields, zap.Int(key, v))
		case int64:
			fields = append(fields, zap.Int64(key, v))
		case float64:
			fields = append(fields, zap.Float64(key, v))
		case bool:
			fields = append(fields, zap.Bool(key, v))
		case error:
			fields = append(fields, zap.Error(v))
		default:
			// Use reflection-based approach for other types
			fields = append(fields, zap.Any(key, v))
		}
	}

	return fields
}
