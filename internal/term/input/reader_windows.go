// SPDX-License-Identifier: MPL-2.0

//go:build windows

package input

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrCanceled is returned by ConsoleReader.ReadEvents after Cancel.
var ErrCanceled = errors.New("console input canceled")

// ErrNotConsole is returned by NewConsoleReader when the file is not a console
// input handle, for example redirected stdin.
var ErrNotConsole = errors.New("input is not a console")

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procReadConsoleInputW    = kernel32.NewProc("ReadConsoleInputW")
	procFlushConsoleInputBuf = kernel32.NewProc("FlushConsoleInputBuffer")
)

const (
	recKey   = 0x0001
	recMouse = 0x0002
	recSize  = 0x0004
	recFocus = 0x0010

	maxRecords = 256
)

// inputRecord mirrors the Win32 INPUT_RECORD: a 2-byte event type, 2 bytes of
// padding, and a 16-byte union.
type inputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

// ConsoleReader reads Windows console input records and decodes them into
// events. Read blocks in a wait on the console handle and a cancel event, so
// Cancel returns promptly.
type ConsoleReader struct {
	dec          ConsoleDecoder
	conin        windows.Handle
	cancelEvent  windows.Handle
	originalMode uint32
	canceled     atomic.Bool
	closeOnce    sync.Once
	closeErr     error
}

// NewConsoleReader attaches to the console input handle of f. It enables
// window input records, and mouse records when mouse is set, and discards
// pending input. Close restores the previous console mode.
func NewConsoleReader(f *os.File, mouse bool) (*ConsoleReader, error) {
	conin := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(conin, &mode); err != nil {
		return nil, ErrNotConsole
	}
	if r, _, err := procFlushConsoleInputBuf.Call(uintptr(conin)); r == 0 {
		return nil, fmt.Errorf("flush console input: %w", err)
	}
	cancelEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("create cancel event: %w", err)
	}
	newMode := uint32(windows.ENABLE_WINDOW_INPUT | windows.ENABLE_EXTENDED_FLAGS)
	if mouse {
		newMode |= windows.ENABLE_MOUSE_INPUT
	}
	if err := windows.SetConsoleMode(conin, newMode); err != nil {
		_ = windows.CloseHandle(cancelEvent)
		return nil, fmt.Errorf("set console mode: %w", err)
	}
	return &ConsoleReader{conin: conin, cancelEvent: cancelEvent, originalMode: mode}, nil
}

// Cancel unblocks a pending ReadEvents, which returns ErrCanceled.
func (r *ConsoleReader) Cancel() bool {
	r.canceled.Store(true)
	return windows.SetEvent(r.cancelEvent) == nil
}

// Close restores the console mode and releases the cancel event. ReadEvents
// must not be running.
func (r *ConsoleReader) Close() error {
	r.closeOnce.Do(func() {
		if err := windows.SetConsoleMode(r.conin, r.originalMode); err != nil {
			r.closeErr = fmt.Errorf("reset console mode: %w", err)
		}
		_ = windows.CloseHandle(r.cancelEvent)
	})
	return r.closeErr
}

// ReadEvents blocks until console input is available and returns the decoded
// events of the records read, at least one.
func (r *ConsoleReader) ReadEvents() ([]Event, error) {
	for {
		if r.canceled.Load() {
			return nil, ErrCanceled
		}
		which, err := windows.WaitForMultipleObjects([]windows.Handle{r.conin, r.cancelEvent}, false, windows.INFINITE)
		if err != nil {
			return nil, fmt.Errorf("wait for console input: %w", err)
		}
		if which != windows.WAIT_OBJECT_0 {
			return nil, ErrCanceled
		}

		var records [maxRecords]inputRecord
		var read uint32
		if rc, _, err := procReadConsoleInputW.Call(
			uintptr(r.conin),
			uintptr(unsafe.Pointer(&records[0])),
			uintptr(len(records)),
			uintptr(unsafe.Pointer(&read)),
		); rc == 0 {
			if errno, ok := err.(syscall.Errno); ok && errno == 0 {
				err = errors.New("ReadConsoleInputW failed")
			}
			return nil, fmt.Errorf("read console input: %w", err)
		}

		var events []Event
		for i := range read {
			events = r.decode(&records[i], events)
		}
		if len(events) > 0 {
			return events, nil
		}
	}
}

func (r *ConsoleReader) decode(rec *inputRecord, events []Event) []Event {
	switch rec.EventType {
	case recKey:
		keyDown := *(*int32)(unsafe.Pointer(&rec.Event[0])) != 0
		events = append(events, r.dec.Key(KeyRecord{
			KeyDown:         keyDown,
			RepeatCount:     *(*uint16)(unsafe.Pointer(&rec.Event[4])),
			VirtualKey:      *(*uint16)(unsafe.Pointer(&rec.Event[6])),
			ScanCode:        *(*uint16)(unsafe.Pointer(&rec.Event[8])),
			Char:            *(*uint16)(unsafe.Pointer(&rec.Event[10])),
			ControlKeyState: *(*uint32)(unsafe.Pointer(&rec.Event[12])),
		})...)
	case recMouse:
		events = append(events, r.dec.Mouse(MouseRecord{
			X:               *(*int16)(unsafe.Pointer(&rec.Event[0])),
			Y:               *(*int16)(unsafe.Pointer(&rec.Event[2])),
			ButtonState:     *(*uint32)(unsafe.Pointer(&rec.Event[4])),
			ControlKeyState: *(*uint32)(unsafe.Pointer(&rec.Event[8])),
			EventFlags:      *(*uint32)(unsafe.Pointer(&rec.Event[12])),
		}))
	case recSize:
		w := *(*int16)(unsafe.Pointer(&rec.Event[0]))
		h := *(*int16)(unsafe.Pointer(&rec.Event[2]))
		if ev := r.dec.Size(w, h); ev != nil {
			events = append(events, ev)
		}
	case recFocus:
		events = append(events, Focus(*(*int32)(unsafe.Pointer(&rec.Event[0])) != 0))
	}
	return events
}
