// SPDX-License-Identifier: MPL-2.0
package terminal

import (
	"time"

	ttyapi "github.com/wippyai/runtime/api/tty"
)

const graphicsProbeID = 0xfffffffe
const graphicsProbeTimeout = 250 * time.Millisecond

// ProbeGraphics uses the existing input reader; it never steals stdin or waits
// on a Lua scheduler worker. Polling distinguishes pending from unsupported.
func (r *InputReader) ProbeGraphics() ttyapi.SurfaceCapabilities {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.stopping {
		return ttyapi.SurfaceCapabilities{Images: "none"}
	}
	if r.graphicsProbeAt.IsZero() {
		r.graphicsProbeAt = time.Now()
		query := []byte("\x1b_Ga=q,i=4294967294,f=24,s=1,v=1;AAAA\x1b\\\x1b[c")
		if n, err := r.output.Write(query); err != nil || n != len(query) {
			r.graphicsMode = "none"
		}
	}
	if r.graphicsMode != "" {
		return ttyapi.SurfaceCapabilities{Images: r.graphicsMode}
	}
	if time.Since(r.graphicsProbeAt) >= graphicsProbeTimeout {
		r.graphicsMode = "none"
		return ttyapi.SurfaceCapabilities{Images: "none"}
	}
	return ttyapi.SurfaceCapabilities{Images: "pending"}
}
func (r *InputReader) graphicsReply(id int, payload []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == graphicsProbeID && !r.graphicsProbeAt.IsZero() && time.Since(r.graphicsProbeAt) < graphicsProbeTimeout {
		r.graphicsMode = "none"
		if string(payload) == "OK" {
			r.graphicsMode = "kitty"
		}
	}
	return true
}
