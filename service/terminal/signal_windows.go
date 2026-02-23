// SPDX-License-Identifier: MPL-2.0

//go:build windows

package terminal

import "context"

func (r *InputReader) sigwinchLoop(ctx context.Context) {
	defer r.wg.Done()
	<-ctx.Done()
}
