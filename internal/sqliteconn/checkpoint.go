// SPDX-License-Identifier: MPL-2.0

package sqliteconn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// checkpointer owns WAL checkpointing for one database file. Writer
// connections run with wal_autocheckpoint=0, so a commit never copies WAL
// frames into the database file; this worker does it instead.
//
// Every interval it checks the WAL file. Within the bound it runs a PASSIVE
// checkpoint on its own connection, which never blocks readers or writers.
// Over the bound (readers pin old snapshots, or writers outpace PASSIVE
// backfill) it escalates to TRUNCATE on the writer pool: the checkpoint takes
// the writer's turn instead of racing it for the write lock, waits through the
// busy handler for readers to leave the WAL and resets the WAL file to zero
// bytes. Writers queue behind it while it runs. Readers that outlive the busy
// timeout can still prevent truncation; later intervals retry. The configured
// threshold is not a hard size limit.
type checkpointer struct {
	failure  error
	closeErr error
	own      *sql.DB
	writer   *sql.DB
	cancel   context.CancelFunc
	done     chan struct{}
	walPath  string
	interval time.Duration
	maxPages int
	once     sync.Once
	mu       sync.Mutex
}

func newCheckpointer(conn *Connector, cfg sqlapi.SQLiteCheckpointConfig) (*checkpointer, error) {
	if cfg.Interval <= 0 {
		return nil, fmt.Errorf("sqlite checkpoint interval must be positive, got %s", cfg.Interval)
	}
	if cfg.MaxWALPages <= 0 {
		return nil, fmt.Errorf("sqlite checkpoint max WAL pages must be positive, got %d", cfg.MaxWALPages)
	}
	own := sql.OpenDB(conn)
	own.SetMaxOpenConns(1)
	own.SetMaxIdleConns(1)
	return &checkpointer{own: own, interval: cfg.Interval, maxPages: cfg.MaxWALPages}, nil
}

// start runs the worker against the writer pool that owns this checkpointer.
func (c *checkpointer) start(writer *sql.DB) {
	ctx, cancel := context.WithCancel(context.Background())
	c.writer = writer
	c.cancel = cancel
	c.done = make(chan struct{})
	go c.run(ctx)
}

func (c *checkpointer) run(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.checkpoint(ctx); err != nil && ctx.Err() == nil {
				c.record(err)
			}
		}
	}
}

// checkpoint backfills the WAL without blocking while it is within the bound
// and truncates it through the writer pool when it exceeds the bound.
func (c *checkpointer) checkpoint(ctx context.Context) error {
	over, err := c.overBound(ctx)
	if err != nil {
		return err
	}
	if over {
		return c.truncate(ctx)
	}
	return exec(ctx, c.own, "PASSIVE")
}

// truncate runs TRUNCATE on a writer-pool connection, taking the next writer
// turn. Without a connection there is no turn to take: the pool is closing
// (DB.Close closes the pool before it closes the connector) or cannot open a
// connection, which its own callers observe; the next run retries.
func (c *checkpointer) truncate(ctx context.Context) error {
	conn, err := c.writer.Conn(ctx)
	if err != nil {
		return nil
	}
	defer conn.Close()
	return exec(ctx, conn, "TRUNCATE")
}

// overBound reports whether the WAL file holds more than maxPages frames.
func (c *checkpointer) overBound(ctx context.Context) (bool, error) {
	if c.walPath == "" {
		var file string
		err := c.own.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&file)
		if err != nil {
			return false, fmt.Errorf("sqlite checkpoint database path: %w", err)
		}
		c.walPath = file + "-wal"
	}
	var pageSize int64
	if err := c.own.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return false, fmt.Errorf("sqlite checkpoint page size: %w", err)
	}
	info, err := os.Stat(c.walPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("sqlite checkpoint WAL size: %w", err)
	}
	return info.Size() > walHeaderSize+int64(c.maxPages)*(walFrameHeaderSize+pageSize), nil
}

// WAL file layout: https://www.sqlite.org/fileformat.html#the_write_ahead_log
const (
	walHeaderSize      = 32
	walFrameHeaderSize = 24
)

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// exec runs one checkpoint on db. A busy result is not a failure: the next
// run retries.
func exec(ctx context.Context, db queryer, mode string) error {
	var busy, frames, backfilled int
	err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint("+mode+")").Scan(&busy, &frames, &backfilled)
	if err != nil {
		return fmt.Errorf("sqlite %s checkpoint: %w", mode, err)
	}
	return nil
}

func (c *checkpointer) record(err error) {
	c.mu.Lock()
	if c.failure == nil {
		c.failure = err
	}
	c.mu.Unlock()
}

// Close stops the worker, runs a final TRUNCATE checkpoint on its own
// connection (the writer pool is closing) and closes that connection. It
// returns the first background failure, if any, together with any shutdown
// failure.
func (c *checkpointer) Close() error {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
			<-c.done
		}
		final := exec(context.Background(), c.own, "TRUNCATE")
		c.mu.Lock()
		failure := c.failure
		c.mu.Unlock()
		c.closeErr = errors.Join(failure, final, c.own.Close())
	})
	return c.closeErr
}
