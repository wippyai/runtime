package httphandler

import (
	"net/http"

	"go.uber.org/zap"
)

const metatableName = "http_handler"

type httpHandler struct {
	r  *http.Request
	rw http.ResponseWriter
}

type Module struct {
	log     *zap.Logger
	handler *httpHandler
}

func New(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}
