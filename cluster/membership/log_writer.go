// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"bytes"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// memberlistLogWriter routes memberlist's standard-library logger output
// through zap, preserving severity. Without this, memberlist [ERR] lines
// (e.g., "Failed to decode user message") get swallowed by io.Discard and
// real bugs hide for as long as no one runs with VeryVerbose. Symptom-only
// noise ([DEBUG]/[INFO]) is dropped unless verbose is enabled.
type memberlistLogWriter struct {
	logger  *zap.Logger
	buf     bytes.Buffer
	mu      sync.Mutex
	verbose bool
}

func newMemberlistLogWriter(logger *zap.Logger, verbose bool) *memberlistLogWriter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &memberlistLogWriter{logger: logger, verbose: verbose}
}

// Write splits incoming bytes on newlines, classifies each line by its
// memberlist severity tag, and emits at the matching zap level.
func (w *memberlistLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)

	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// Incomplete line — push it back and wait for the rest.
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		w.emit(strings.TrimRight(line, "\r\n"))
	}
	return len(p), nil
}

func (w *memberlistLogWriter) emit(line string) {
	if line == "" {
		return
	}
	// memberlist's stdlib logger prefixes severity like "[ERR]" / "[WARN]" /
	// "[INFO]" / "[DEBUG]". The standard log.Logger also prefixes a date and
	// time — strip those by matching the bracketed tag.
	switch {
	case strings.Contains(line, "[ERR]"), strings.Contains(line, "[ERROR]"):
		w.logger.Error("memberlist", zap.String("msg", line))
	case strings.Contains(line, "[WARN]"):
		w.logger.Warn("memberlist", zap.String("msg", line))
	case strings.Contains(line, "[INFO]"):
		if w.verbose {
			w.logger.Info("memberlist", zap.String("msg", line))
		}
	case strings.Contains(line, "[DEBUG]"):
		if w.verbose {
			w.logger.Debug("memberlist", zap.String("msg", line))
		}
	default:
		// Unknown severity — surface as info in verbose, debug otherwise.
		if w.verbose {
			w.logger.Info("memberlist", zap.String("msg", line))
		}
	}
}
