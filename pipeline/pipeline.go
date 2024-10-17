package pipeline

import "go.uber.org/zap"

type Pipeline struct {
	log *zap.Logger
}

func NewPipeline(log *zap.Logger) *Pipeline {
	return &Pipeline{
		log: log,
	}
}
