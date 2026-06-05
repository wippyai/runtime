// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pglogrepl"
)

type Checkpointer interface {
	Load(ctx context.Context, slot string) (pglogrepl.LSN, bool, error)
	Save(ctx context.Context, slot string, lsn pglogrepl.LSN) error
	Delete(ctx context.Context, slot string) error
}

type MemoryCheckpointer struct {
	pos map[string]pglogrepl.LSN
	mu  sync.Mutex
}

func NewMemoryCheckpointer() *MemoryCheckpointer {
	return &MemoryCheckpointer{pos: make(map[string]pglogrepl.LSN)}
}

func (m *MemoryCheckpointer) Load(_ context.Context, slot string) (pglogrepl.LSN, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lsn, ok := m.pos[slot]
	return lsn, ok, nil
}

func (m *MemoryCheckpointer) Save(_ context.Context, slot string, lsn pglogrepl.LSN) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pos[slot] = lsn
	return nil
}

func (m *MemoryCheckpointer) Delete(_ context.Context, slot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pos, slot)
	return nil
}

type DBCheckpointer struct {
	db *sql.DB
}

func NewDBCheckpointer(ctx context.Context, db *sql.DB) (*DBCheckpointer, error) {
	c := &DBCheckpointer{db: db}
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS wippy_cdc_offsets (
			slot       text PRIMARY KEY,
			lsn        text NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, fmt.Errorf("ensure offsets table: %w", err)
	}
	return c, nil
}

func (c *DBCheckpointer) Load(ctx context.Context, slot string) (pglogrepl.LSN, bool, error) {
	var raw string
	err := c.db.QueryRowContext(ctx, `SELECT lsn FROM wippy_cdc_offsets WHERE slot = $1`, slot).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load offset: %w", err)
	}
	lsn, err := pglogrepl.ParseLSN(raw)
	if err != nil {
		return 0, false, fmt.Errorf("parse stored lsn %q: %w", raw, err)
	}
	return lsn, true, nil
}

func (c *DBCheckpointer) Save(ctx context.Context, slot string, lsn pglogrepl.LSN) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO wippy_cdc_offsets (slot, lsn, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (slot) DO UPDATE SET lsn = EXCLUDED.lsn, updated_at = now()`,
		slot, lsn.String())
	if err != nil {
		return fmt.Errorf("save offset: %w", err)
	}
	return nil
}

func (c *DBCheckpointer) Delete(ctx context.Context, slot string) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM wippy_cdc_offsets WHERE slot = $1`, slot); err != nil {
		return fmt.Errorf("delete offset: %w", err)
	}
	return nil
}
