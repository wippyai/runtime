package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/aws/s3"
	"github.com/ponyruntime/pony/system/eventbus"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

type s3Plugin struct {
	handler eventbus.EventHandler
}

func (p *s3Plugin) Name() string        { return bootpkg.S3 }
func (p *s3Plugin) Phase() boot.Phase   { return boot.PostInit }
func (p *s3Plugin) DependsOn() []string { return nil }

func (p *s3Plugin) Load(ctx context.Context) (context.Context, error) {
	logger := logapi.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)
	bus := event.GetBus(ctx)

	manager := s3.NewManager(
		bus,
		dtt,
		logger.Named("cloudstorage.s3"),
	)

	p.handler = reghandler.NewRegistryHandler("cloudstorage.s3", manager)
	return ctx, nil
}

func (p *s3Plugin) Handler() eventbus.EventHandler {
	return p.handler
}

func init() {
	bootpkg.MustRegister(&s3Plugin{})
}
