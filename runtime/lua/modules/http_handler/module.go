package httphandler

import (
	"go.uber.org/zap"
)

const metatableName = "http_handler"

type Module struct {
	log *zap.Logger
}

func New(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

func (m *Module) Name() string {
	return "http_handler"
}
