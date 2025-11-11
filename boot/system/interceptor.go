package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/interceptor"
)

type interceptorPlugin struct{}

func (p *interceptorPlugin) Name() string        { return bootpkg.Interceptor }
func (p *interceptorPlugin) Phase() boot.Phase   { return boot.Init }
func (p *interceptorPlugin) DependsOn() []string { return []string{bootpkg.EventBus, bootpkg.Logger} }

func (p *interceptorPlugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	bus := event.GetBus(ctx)

	interceptorReg := interceptor.NewInterceptorRegistry(bus, logger.Named("interceptor"))
	return apiinterceptor.WithInterceptor(ctx, interceptorReg), nil
}

func (p *interceptorPlugin) Start(ctx context.Context) error {
	interceptorReg := apiinterceptor.GetInterceptor(ctx)
	return interceptorReg.Start(ctx)
}

func (p *interceptorPlugin) Stop(ctx context.Context) error {
	interceptorReg := apiinterceptor.GetInterceptor(ctx)
	return interceptorReg.Stop()
}

func init() {
	bootpkg.MustRegister(&interceptorPlugin{})
}
