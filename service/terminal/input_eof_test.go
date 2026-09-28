//go:build !windows

// SPDX-License-Identifier: MPL-2.0
package terminal

import (
	"io"
	"testing"
	"time"

	"github.com/creack/pty"
	tty "github.com/wippyai/runtime/api/tty"
)

func TestPhysicalReaderPeerCloseCompletesCleanup(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	events := make(chan tty.Event, 4)
	raw := NewRawManager(slave)
	reader := NewEventInputReader(slave, io.Discard, raw, func(event tty.Event) {
		select {
		case events <- event:
		default:
		}
	})
	if err := reader.Start(); err != nil {
		t.Fatal(err)
	}
	defer reader.Stop()
	select {
	case event := <-events:
		if event.Type != "start" {
			t.Fatalf("first event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing start event")
	}
	master.Close()
	select {
	case <-reader.Done():
	case <-time.After(time.Second):
		t.Fatal("peer close did not finish reader cleanup")
	}
	if reader.Err() == nil {
		t.Fatal("peer close not reported")
	}
	if raw.Enabled() {
		t.Fatal("reader completion left raw mode owned")
	}
}
