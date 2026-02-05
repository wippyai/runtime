package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/runtime/lua/lsp"
)

// lspServiceKey is the context key for the LSP service.
type lspServiceKey struct{}

// SetLSPService stores the LSP service in context.
func SetLSPService(ctx context.Context, svc *lsp.Service) context.Context {
	return context.WithValue(ctx, lspServiceKey{}, svc)
}

// GetLSPService retrieves the LSP service from context.
func GetLSPService(ctx context.Context) *lsp.Service {
	if svc, ok := ctx.Value(lspServiceKey{}).(*lsp.Service); ok {
		return svc
	}
	return nil
}

// LSP creates the LSP server component.
func LSP() boot.Component {
	var svc *lsp.Service

	return boot.New(boot.P{
		Name:      LSPName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			cm := GetCodeManager(ctx)
			cfg := boot.GetConfig(ctx)

			lspCfg := lsp.DefaultConfig()

			if cfg != nil {
				lspSub := cfg.Sub("lsp")
				if lspSub != nil {
					lspCfg.Enabled = lspSub.GetBool("enabled", lspCfg.Enabled)
					lspCfg.Address = lspSub.GetString("address", lspCfg.Address)
					lspCfg.MaxMessageBytes = lspSub.GetInt("max_message_bytes", lspCfg.MaxMessageBytes)
					lspCfg.HTTPEnabled = lspSub.GetBool("http_enabled", lspCfg.HTTPEnabled)
					lspCfg.HTTPAddress = lspSub.GetString("http_address", lspCfg.HTTPAddress)
					lspCfg.HTTPPath = lspSub.GetString("http_path", lspCfg.HTTPPath)
					lspCfg.HTTPAllowOrigin = lspSub.GetString("http_allow_origin", lspCfg.HTTPAllowOrigin)
				}
			}
			lspCfg.Validate()

			svc = lsp.New(lspCfg, logger, bus, cm)
			ctx = SetLSPService(ctx, svc)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if svc != nil {
				return svc.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if svc != nil {
				return svc.Stop()
			}
			return nil
		},
	})
}
