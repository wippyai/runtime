// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"io"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// SurfaceOptions describe presentation behavior. Physical ports may translate
// these to terminal modes; virtual ports retain them as surface metadata.
type SurfaceOptions struct {
	AlternateScreen bool
	HideCursor      bool
	Synchronized    bool
}

type PresentStats struct {
	Rows        int
	ChangedRows int
	Bytes       int
}

// Cursor is terminal cursor state in zero-based surface coordinates.
type Cursor struct {
	Column  int
	Row     int
	Visible bool
}

// Frame augments surface rows with optional terminal state. A nil Cursor lets
// a renderer preserve its configured cursor behavior.
type Frame struct {
	Cursor *Cursor
	Rows   []string
}

type RawController interface {
	Enable() error
	Disable() error
	Reset() error
	Enabled() bool
}

// Surface is the presentation side of a terminal port.
type Surface interface {
	// Present atomically publishes cells and optional cursor state. A nil cursor
	// preserves the last explicit cursor state.
	Present(Frame) (PresentStats, error)
	// Invalidate forgets backend presentation state without changing the last
	// published frame. The next Present must commit even if its content is
	// otherwise unchanged.
	Invalidate()
	// Close releases presentation ownership and must be safe to call more than
	// once. Callers receive the result of the first close attempt.
	Close() error
}

// Port is the minimal process-scoped terminal attachment. Implementations must
// be safe to close more than once. Byte streams and raw mode are optional and
// deliberately live on StreamPort rather than being faked by virtual ports.
type Port interface {
	InputController() InputController
	// OpenSurface acquires the port's exclusive presentation lease. A port has
	// one producer, so concurrent surfaces are rejected until the open surface
	// is closed.
	OpenSurface(SurfaceOptions) (Surface, error)
	Close() error
}

// StreamPort augments a Port with physical byte streams and raw-mode control.
// Host terminals implement it; structured virtual viewports need not.
type StreamPort interface {
	Port
	RawController() RawController
	Reader() io.Reader
	Output() io.Writer
	ErrorOutput() io.Writer
}

// Binding delays grant redemption until the destination process frame exists.
type Binding interface {
	Resolve(context.Context) (Port, error)
	Close() error
}

var portKey = &ctxapi.Key{Name: "tty.port"} // deliberately non-inheritable

func PortKey() *ctxapi.Key { return portKey }

func PortPair(port Port) ctxapi.Pair { return ctxapi.Pair{Key: portKey, Value: port} }

func BindingPair(binding Binding) ctxapi.Pair {
	return ctxapi.Pair{Key: portKey, Value: binding}
}

func WithPort(ctx context.Context, port Port) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(portKey, port)
}

// GetPort returns the frame-owned port, redeeming a lazy binding once.
func GetPort(ctx context.Context) (Port, error) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, nil
	}
	v, ok := fc.Get(portKey)
	if !ok {
		return nil, nil
	}
	if port, ok := v.(Port); ok {
		return port, nil
	}
	if binding, ok := v.(Binding); ok {
		return binding.Resolve(ctx)
	}
	return nil, ErrInvalidPort
}
