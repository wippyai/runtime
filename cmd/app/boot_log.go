// SPDX-License-Identifier: MPL-2.0

package app

import "go.uber.org/zap"

// BootLogger supplies the host's structured diagnostic logger before the
// runtime event bus exists. Hosts that omit it keep ordinary silent startup.
type BootLogger interface {
	BootLogger() *zap.Logger
}

func bootPhase(e Executable, name, stage string) {
	if host, ok := e.Host.(BootLogger); ok {
		if log := host.BootLogger(); log != nil {
			log.Info("Boot phase", zap.String("phase", name), zap.String("stage", stage))
		}
	}
}
