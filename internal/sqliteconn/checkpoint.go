// SPDX-License-Identifier: MPL-2.0

package sqliteconn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// checkpointer owns WAL checkpointing for one database file. Writer
// connections run with wal_autocheckpoint=0, so a commit never copies WAL
// frames into the database file; this worker does it instead.
//
// Every interval it checks the WAL size and runs PASSIVE on its own
// connection, then maintains application-attached databases.
// Over the bound it gives PASSIVE bounded writer turns to catch up, yielding
// to application transactions between turns even while storage work continues.
// It only attempts TRUNCATE after verifying that all frames are backfilled
// with the writer reserved, so TRUNCATE never copies a new backlog.
// Reader lock waits use a short maintenance timeout; the configured threshold
// is not a hard size limit.
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
	opened   atomic.Bool
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
	// Bound the maintenance wait independently of application queries. A short
	// wait lets brief reader snapshots leave without allowing long readers to
	// occupy the writer turn for the application's busy timeout.
	// Keep this policy separate from application connections,
	// including when the DSN uses go-sqlite3's _timeout alias.
	path, query, _ := strings.Cut(conn.dsn, "?")
	params, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parse sqlite checkpoint DSN: %w", err)
	}
	params.Del("_timeout")
	params.Set("_busy_timeout", fmt.Sprint(checkpointBusyTimeout.Milliseconds()))
	own := sql.OpenDB(&Connector{driver: conn.driver, dsn: path + "?" + params.Encode()})
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
			if !c.opened.Load() {
				continue
			}
			if err := c.checkpoint(ctx); err != nil && ctx.Err() == nil {
				c.record(err)
			}
		}
	}
}

// checkpoint backfills on the maintenance connection. Over the size bound,
// writer turns are time-sliced independently of the checkpoint's storage work.
func (c *checkpointer) checkpoint(ctx context.Context) error {
	over, err := c.overBound(ctx)
	if err != nil {
		return err
	}
	var conn *sql.Conn
	if over {
		conn, err = c.checkpointWithWriterTurns(ctx)
	} else {
		if err := exec(ctx, c.own, "PASSIVE"); err != nil {
			return err
		}
		conn, err = c.writer.Conn(ctx)
		if err != nil {
			return nil // The application pool owns connection/open errors.
		}
	}
	if err != nil {
		return err
	}
	if conn == nil {
		return nil // The application pool is closing.
	}
	defer conn.Close()
	return checkpointAttachments(ctx, c.own, conn)
}

// checkpointWithWriterTurns gives backfill time to catch up under sustained
// writes without lending the application connection for an entire I/O call.
// PASSIVE never takes SQLite's writer lock, so returning the pool connection
// lets queued transactions run even if the checkpoint is still copying/syncing.
func (c *checkpointer) checkpointWithWriterTurns(ctx context.Context) (*sql.Conn, error) {
	type result struct {
		err       error
		remaining int
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := c.writer.Conn(ctx)
	if err != nil {
		return nil, nil
	}
	done := make(chan result, 1)
	go func() {
		remaining, err := checkpointSchema(workCtx, c.own, "main", "PASSIVE")
		done <- result{err: err, remaining: remaining}
	}()
	for {
		timer := time.NewTimer(checkpointWriterSlice)
		select {
		case completed := <-done:
			timer.Stop()
			if completed.err != nil {
				_ = conn.Close()
				return nil, completed.err
			}
			if completed.remaining == 0 {
				// A commit may have arrived after PASSIVE completed while we
				// waited for this turn. NOOP checks without copying or syncing.
				remaining, err := checkpointSchema(ctx, c.own, "main", "NOOP")
				if err == nil && remaining == 0 {
					err = exec(ctx, c.own, "TRUNCATE")
				}
				if err != nil {
					_ = conn.Close()
					return nil, err
				}
			}
			return conn, nil
		case <-timer.C:
			// Close hands this physical connection to a queued application
			// operation before we request another turn through the same pool.
			_ = conn.Close()
			conn, err = c.writer.Conn(ctx)
			if err != nil {
				cancel()
				<-done
				return nil, nil
			}
		case <-ctx.Done():
			timer.Stop()
			_ = conn.Close()
			cancel()
			<-done
			return nil, ctx.Err()
		}
	}
}

// checkpointAttachments discovers writable WAL attachments on their physical
// application connection, then releases that turn before backfilling through
// temporary attachments on the maintenance connection.
func checkpointAttachments(ctx context.Context, own *sql.DB, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return err
	}
	type attachment struct{ schema, file string }
	var attachments []attachment
	for rows.Next() {
		var index int
		var schema, file string
		if err := rows.Scan(&index, &schema, &file); err != nil {
			_ = rows.Close()
			return err
		}
		if schema != "main" && schema != "temp" && file != "" {
			attachments = append(attachments, attachment{schema, file})
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	writable := attachments[:0]
	for _, attached := range attachments {
		remaining, err := checkpointSchema(ctx, conn, attached.schema, "NOOP")
		if err != nil {
			if checkpointReadOnly(err) {
				continue
			}
			return err
		}
		if remaining >= 0 {
			writable = append(writable, attached)
		}
	}
	if err := conn.Close(); err != nil {
		return err
	}
	if len(writable) == 0 {
		return nil
	}
	maintenance, err := own.Conn(ctx)
	if err != nil {
		return err
	}
	defer maintenance.Close()
	for _, attached := range writable {
		quoted := `"` + strings.ReplaceAll(attached.schema, `"`, `""`) + `"`
		// URI quoting preserves filenames containing '?' or '#'; mode=rw
		// avoids recreating a file removed after discovery.
		path := filepath.ToSlash(attached.file)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path // Windows drive paths need file:///C:/... form.
		}
		uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}).String()
		if _, err := maintenance.ExecContext(ctx, "ATTACH DATABASE ? AS "+quoted, uri); err != nil {
			return err
		}
		checkpointErr := execSchema(ctx, maintenance, attached.schema, "PASSIVE")
		// Clean up the pooled attachment even when shutdown canceled the checkpoint.
		_, detachErr := maintenance.ExecContext(context.Background(), "DETACH DATABASE "+quoted)
		if err := errors.Join(checkpointErr, detachErr); err != nil {
			return err
		}
	}
	return nil
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

// A bounded wait lets short snapshots finish rather than starving truncation
// under sustained traffic. The writer pool's normal busy timeout is unchanged.
const checkpointBusyTimeout = 50 * time.Millisecond

// A scheduling slice, not a query or transaction timeout: storage work keeps
// running on the maintenance connection after the application writer is yielded.
const checkpointWriterSlice = 50 * time.Millisecond

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// exec runs one checkpoint on db. A busy result is not a failure: the next
// run retries.
func exec(ctx context.Context, db queryer, mode string) error {
	return execSchema(ctx, db, "main", mode)
}

func execSchema(ctx context.Context, db queryer, schema, mode string) error {
	_, err := checkpointSchema(ctx, db, schema, mode)
	return err
}

// checkpointSchema returns the remaining WAL frames, or -1 when busy or not
// in WAL mode. NOOP inspects this count without doing checkpoint storage work.
func checkpointSchema(ctx context.Context, db queryer, schema, mode string) (int, error) {
	var busy, frames, backfilled int
	quoted := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
	err := db.QueryRowContext(ctx, "PRAGMA "+quoted+".wal_checkpoint("+mode+")").Scan(&busy, &frames, &backfilled)
	if err != nil {
		if checkpointBusy(err) {
			return -1, nil
		}
		return -1, fmt.Errorf("sqlite %s checkpoint: %w", mode, err)
	}
	if busy != 0 || frames < 0 || backfilled < 0 {
		return -1, nil
	}
	return frames - backfilled, nil
}

func (c *checkpointer) record(err error) {
	if checkpointBusy(err) {
		return
	}
	c.mu.Lock()
	if c.failure == nil {
		c.failure = err
	}
	c.mu.Unlock()
}

// Close stops the worker, attempts a final TRUNCATE checkpoint on its own
// connection (the writer pool is closing) and closes that connection. It
// returns the first background failure, if any, together with any shutdown
// failure.
func (c *checkpointer) Close() error {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
			<-c.done
		}
		var final error
		if c.opened.Load() {
			final = exec(context.Background(), c.own, "TRUNCATE")
		}
		c.mu.Lock()
		failure := c.failure
		c.mu.Unlock()
		c.closeErr = errors.Join(failure, final, c.own.Close())
	})
	return c.closeErr
}
