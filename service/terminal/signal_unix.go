// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package terminal

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func (r *InputReader) sigwinchLoop(ctx context.Context) {
	defer r.wg.Done()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			r.emitResize()
		}
	}
}
