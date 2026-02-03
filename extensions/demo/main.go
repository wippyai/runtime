package main

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	extensionapi "github.com/wippyai/runtime/api/extension"
	logapi "github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap"
)

// WippyExtension is the exported extension manifest.
var WippyExtension = extensionapi.Manifest{
	Name:       "demo",
	Version:    "0.1.0",
	ABI:        extensionapi.ABI,
	Components: []boot.Component{demoComponent()},
}

func demoComponent() boot.Component {
	return boot.New(boot.P{
		Name: "extension.demo",
		Load: func(ctx context.Context) (context.Context, error) {
			demoLogger(ctx).Info("demo extension loaded")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			demoLogger(ctx).Info("demo extension started")
			return nil
		},
	})
}

func demoLogger(ctx context.Context) *zap.Logger {
	logger := logapi.GetLogger(ctx)
	if logger == nil {
		logger = zap.NewNop()
	}
	return logger.Named("extension.demo")
}
