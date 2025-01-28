package docker

import "go.uber.org/zap"

// Runner is a struct that represents command runner
// a single instance of Runner is created for each command
type Runner struct {
	log *zap.Logger
}

func NewRunner(log *zap.Logger) *Runner {
	return &Runner{
		log: log,
	}
}
