// SPDX-License-Identifier: MPL-2.0

// Package tty provides terminal I/O command handlers for the dispatcher system.
package tty

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var (
	errNoTerminalContext = errors.New("no terminal context")
	errNoRawController   = errors.New("raw terminal control unavailable")
	errNoInputController = errors.New("input controller unavailable")
	errDispatcherBusy    = errors.New("terminal command queue capacity exceeded")
	errDispatcherStarted = errors.New("terminal dispatcher already started")
)

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithWorkers sets the worker count for each of the read and control lanes.
func WithWorkers(n int) Option {
	return func(d *Dispatcher) {
		if n > 0 {
			d.workers = n
		}
	}
}

// Dispatcher isolates blocking stream reads from terminal control commands.
// Each lane has a bounded queue and the configured worker count.
type Dispatcher struct {
	ctx         context.Context
	reads       chan job
	jobs        chan job
	cancel      context.CancelFunc
	asyncSlots  chan struct{}
	wg          sync.WaitGroup
	workers     int
	asyncMu     sync.Mutex
	lifecycleMu sync.Mutex
	stopping    bool
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
}

// NewDispatcher creates a terminal I/O dispatcher with one worker per lane.
func NewDispatcher(opts ...Option) *Dispatcher {
	d := &Dispatcher{workers: 1, asyncSlots: make(chan struct{}, 128)}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Start initializes the worker pool.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()
	d.asyncMu.Lock()
	defer d.asyncMu.Unlock()
	if d.jobs != nil {
		return errDispatcherStarted
	}
	d.stopping = false
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)
	d.reads = make(chan job, d.workers*2)
	for i := 0; i < d.workers; i++ {
		d.wg.Add(2)
		go d.worker(d.jobs)
		go d.worker(d.reads)
	}
	return nil
}

// Stop shuts down the dispatcher and drains pending jobs.
func (d *Dispatcher) Stop(_ context.Context) error {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()
	d.asyncMu.Lock()
	d.stopping = true
	cancel := d.cancel
	jobs, reads := d.jobs, d.reads
	d.cancel = nil
	d.ctx = nil
	d.jobs, d.reads = nil, nil
	if jobs != nil {
		close(jobs)
		close(reads)
	}
	d.asyncMu.Unlock()
	if cancel != nil {
		cancel()
	}
	d.wg.Wait()
	return nil
}

func (d *Dispatcher) worker(jobs <-chan job) {
	defer d.wg.Done()
	for j := range jobs {
		d.execute(j)
	}
}

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	j := job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.asyncMu.Lock()
	if d.jobs == nil {
		stopping := d.stopping
		d.asyncMu.Unlock()
		if stopping {
			return ttyapi.ErrServiceUnavailable
		}
		d.execute(j)
		return nil
	}
	if d.stopping {
		d.asyncMu.Unlock()
		return ttyapi.ErrServiceUnavailable
	}
	if err := ctx.Err(); err != nil {
		d.asyncMu.Unlock()
		return err
	}
	if err := d.ctx.Err(); err != nil {
		d.asyncMu.Unlock()
		return err
	}

	queue := d.jobs
	switch cmd.(type) {
	case ttyapi.ReadCmd, ttyapi.ReadLineCmd:
		queue = d.reads
	}
	select {
	case queue <- j:
		d.asyncMu.Unlock()
		return nil
	case <-d.ctx.Done():
		err := d.ctx.Err()
		d.asyncMu.Unlock()
		return err
	default:
		d.asyncMu.Unlock()
		return errDispatcherBusy
	}
}

func (d *Dispatcher) execute(j job) {
	port, resolveErr := ttyapi.GetPort(j.ctx)
	if resolveErr != nil {
		j.receiver.CompleteYield(j.tag, nil, resolveErr)
		return
	}
	if port == nil {
		j.receiver.CompleteYield(j.tag, nil, errNoTerminalContext)
		return
	}
	streams, _ := port.(ttyapi.StreamPort)

	switch c := j.cmd.(type) {
	case ttyapi.ReadCmd:
		if streams == nil || streams.Reader() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoTerminalContext)
			return
		}
		size := c.Size
		if size <= 0 {
			size = ttyapi.DefaultReadSize
		}
		buf := make([]byte, size)
		n, err := streams.Reader().Read(buf)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, buf[:n], nil)

	case ttyapi.ReadLineCmd:
		if streams == nil || streams.Reader() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoTerminalContext)
			return
		}
		reader := bufio.NewReader(streams.Reader())
		line, err := reader.ReadString('\n')
		if err != nil {
			if len(line) > 0 {
				j.receiver.CompleteYield(j.tag, trimLine(line), nil)
				return
			}
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, trimLine(line), nil)

	case ttyapi.RawEnableCmd:
		if streams == nil || streams.RawController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoRawController)
			return
		}
		if err := streams.RawController().Enable(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.RawDisableCmd:
		if streams == nil || streams.RawController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoRawController)
			return
		}
		if err := streams.RawController().Disable(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.StartInputCmd:
		if port.InputController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		if err := port.InputController().Start(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.StopInputCmd:
		if port.InputController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		if err := port.InputController().Stop(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.ScreenSizeCmd:
		if port.InputController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		cols, rows, err := port.InputController().ScreenSize()
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, []int{cols, rows}, nil)

	case ttyapi.EnableMouseCmd:
		if port.InputController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		port.InputController().EnableMouse()
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.DisableMouseCmd:
		if port.InputController() == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		port.InputController().DisableMouse()
		j.receiver.CompleteYield(j.tag, true, nil)

	default:
		j.receiver.CompleteYield(j.tag, nil, fmt.Errorf("unknown tty command: %T", j.cmd))
	}
}

func trimLine(line string) string {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}

func (d *Dispatcher) handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	return d.submit(ctx, cmd, tag, receiver)
}

// RegisterAll registers all terminal I/O handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(ttyapi.ViewportIO, dispatcher.HandlerFunc(d.handleViewportIO))
	h := dispatcher.HandlerFunc(d.handle)
	register(ttyapi.Read, h)
	register(ttyapi.ReadLine, h)
	register(ttyapi.RawEnable, h)
	register(ttyapi.RawDisable, h)
	register(ttyapi.StartInput, h)
	register(ttyapi.StopInput, h)
	register(ttyapi.ScreenSize, h)
	register(ttyapi.EnableMouse, h)
	register(ttyapi.DisableMouse, h)
}

// Remote operations must never occupy the terminal read worker or a Lua
// scheduler worker. Admission is bounded and cancellation releases the slot.
func (d *Dispatcher) handleViewportIO(ctx context.Context, command dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := command.(ttyapi.ViewportIOCmd)
	d.asyncMu.Lock()
	if d.stopping {
		d.asyncMu.Unlock()
		return ttyapi.ErrServiceUnavailable
	}
	select {
	case d.asyncSlots <- struct{}{}:
	default:
		d.asyncMu.Unlock()
		return ttyapi.ErrMeshBusy
	}
	d.wg.Add(1)
	lifecycleCtx := d.ctx
	d.asyncMu.Unlock()
	go func() {
		defer d.wg.Done()
		defer func() { <-d.asyncSlots }()
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		if lifecycleCtx != nil {
			stop := context.AfterFunc(lifecycleCtx, cancel)
			defer stop()
		}
		var result any
		var err error
		switch c.Operation {
		case "image_import":
			provider, ok := ttyapi.GetService(runCtx).(interface {
				ImageStore() *ttyapi.ImageStore
			})
			if !ok {
				err = ttyapi.ErrServiceUnavailable
			} else {
				result, err = provider.ImageStore().ImportPNG(c.ImageData)
			}
		case "capture":
			if c.CaptureSource == nil {
				err = ttyapi.ErrInvalidPort
			} else {
				result, err = c.CaptureSource.Capture(runCtx)
			}
		case "attach":
			service := ttyapi.GetService(runCtx)
			if service == nil {
				err = ttyapi.ErrServiceUnavailable
			} else {
				result, err = service.Attach(runCtx, c.Handle)
			}
		case "send":
			if c.View == nil {
				err = ttyapi.ErrInvalidPort
			} else {
				err = c.View.SendContext(runCtx, c.Event)
				result = true
			}
		case "resize":
			if c.View == nil {
				err = ttyapi.ErrInvalidPort
			} else {
				err = c.View.ResizeContext(runCtx, c.Width, c.Height)
				result = true
			}
		default:
			err = ttyapi.ErrInvalidPort
		}

		if owned, ok := result.(interface{ Close() error }); ok && (c.Operation == "capture" || c.Operation == "image_import") {
			transfer := ttyapi.ImageIOResult{}
			if img, ok := result.(*ttyapi.Image); ok {
				transfer.Image = img
			}
			if capture, ok := result.(*ttyapi.Capture); ok {
				transfer.Capture = capture
			}
			if store := resource.GetStore(runCtx); store != nil {
				transfer.Cancel = store.AddCleanup(owned.Close)
			}
			result = transfer
			if runCtx.Err() != nil {
				if transfer.Cancel != nil {
					transfer.Cancel()
				}
				_ = owned.Close()
				result = nil
				err = runCtx.Err()
			}
		}

		receiver.CompleteYield(tag, result, err)
	}()
	return nil
}
