// Package tty provides terminal I/O command handlers for the dispatcher system.
package tty

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var (
	errNoTerminalContext  = errors.New("no terminal context")
	errNoRawController    = errors.New("raw terminal control unavailable")
	errNoInputController  = errors.New("input controller unavailable")
)

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithWorkers sets the number of worker goroutines.
func WithWorkers(n int) Option {
	return func(d *Dispatcher) {
		if n > 0 {
			d.workers = n
		}
	}
}

// Dispatcher handles terminal I/O commands via an async worker pool.
type Dispatcher struct {
	ctx     context.Context
	jobs    chan job
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	workers int
}

type job struct {
	ctx      context.Context
	cmd      dispatcher.Command
	receiver dispatcher.ResultReceiver
	tag      uint64
}

// NewDispatcher creates a terminal I/O dispatcher with default 1 worker.
func NewDispatcher(opts ...Option) *Dispatcher {
	d := &Dispatcher{workers: 1}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Start initializes the worker pool.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
	return nil
}

// Stop shuts down the dispatcher and drains pending jobs.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.cancel != nil {
		d.cancel()
	}
	if d.jobs != nil {
		close(d.jobs)
	}
	d.wg.Wait()
	d.jobs = nil
	d.cancel = nil
	return nil
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for j := range d.jobs {
		d.execute(j)
	}
}

func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) {
	j := job{ctx: ctx, cmd: cmd, tag: tag, receiver: receiver}
	if d.jobs == nil {
		d.execute(j)
		return
	}

	select {
	case d.jobs <- j:
	case <-d.ctx.Done():
	default:
		d.execute(j)
	}
}

func (d *Dispatcher) execute(j job) {
	tc := terminal.GetTerminalContext(j.ctx)
	if tc == nil {
		j.receiver.CompleteYield(j.tag, nil, errNoTerminalContext)
		return
	}

	switch c := j.cmd.(type) {
	case ttyapi.ReadCmd:
		if tc.Stdin == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoTerminalContext)
			return
		}
		size := c.Size
		if size <= 0 {
			size = ttyapi.DefaultReadSize
		}
		buf := make([]byte, size)
		n, err := tc.Stdin.Read(buf)
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, buf[:n], nil)

	case ttyapi.ReadLineCmd:
		if tc.Stdin == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoTerminalContext)
			return
		}
		reader := bufio.NewReader(tc.Stdin)
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
		if tc.Raw == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoRawController)
			return
		}
		if err := tc.Raw.Enable(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.RawDisableCmd:
		if tc.Raw == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoRawController)
			return
		}
		if err := tc.Raw.Disable(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.StartInputCmd:
		if tc.Input == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		if err := tc.Input.Start(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.StopInputCmd:
		if tc.Input == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		if err := tc.Input.Stop(); err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.ScreenSizeCmd:
		if tc.Input == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		cols, rows, err := tc.Input.ScreenSize()
		if err != nil {
			j.receiver.CompleteYield(j.tag, nil, err)
			return
		}
		j.receiver.CompleteYield(j.tag, []int{cols, rows}, nil)

	case ttyapi.EnableMouseCmd:
		if tc.Input == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		tc.Input.EnableMouse()
		j.receiver.CompleteYield(j.tag, true, nil)

	case ttyapi.DisableMouseCmd:
		if tc.Input == nil {
			j.receiver.CompleteYield(j.tag, nil, errNoInputController)
			return
		}
		tc.Input.DisableMouse()
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
	d.submit(ctx, cmd, tag, receiver)
	return nil
}

// RegisterAll registers all terminal I/O handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
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
