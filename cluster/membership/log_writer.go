// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"bytes"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// memberlistLogWriter routes memberlist's standard-library logger output
// through zap with runtime-oriented severity. Without this, memberlist [ERR]
// lines (e.g., "Failed to decode user message") get swallowed by io.Discard
// and real bugs hide for as long as no one runs with VeryVerbose. Some
// memberlist [ERR] lines are normal SWIM failure-detection symptoms under
// partitions or packet loss; those are telemetry signals, not runtime errors,
// and are downgraded so chaos/network churn does not poison the error-log SLO.
// Symptom-only noise ([DEBUG]/[INFO]) is dropped unless verbose is enabled.
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
	switch memberlistZapLevel(line) {
	case zapcore.ErrorLevel:
		w.logger.Error("memberlist", zap.String("msg", line))
	case zapcore.WarnLevel:
		w.logger.Warn("memberlist", zap.String("msg", line))
	case zapcore.InfoLevel:
		if w.verbose {
			w.logger.Info("memberlist", zap.String("msg", line))
		}
	case zapcore.DebugLevel:
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

func memberlistZapLevel(line string) zapcore.Level {
	// memberlist's stdlib logger prefixes severity like "[ERR]" / "[WARN]" /
	// "[INFO]" / "[DEBUG]". The standard log.Logger can also prefix a date and
	// time, so classify by matching the bracketed tag anywhere in the line.
	switch {
	case strings.Contains(line, "[ERR]"), strings.Contains(line, "[ERROR]"):
		if isExpectedMemberlistNetworkFailure(line) {
			return zapcore.WarnLevel
		}
		return zapcore.ErrorLevel
	case strings.Contains(line, "[WARN]"):
		return zapcore.WarnLevel
	case strings.Contains(line, "[INFO]"):
		return zapcore.InfoLevel
	case strings.Contains(line, "[DEBUG]"):
		return zapcore.DebugLevel
	default:
		return zapcore.InvalidLevel
	}
}

func isExpectedMemberlistNetworkFailure(line string) bool {
	msg := strings.ToLower(line)
	expected := [...]string{
		"failed fallback tcp ping",
		"failed to send gossip",
		"failed to send indirect ping",
		"failed to send indirect udp ping",
		"failed to send udp ping",
		"failed to send udp compound ping",
	}
	for _, needle := range expected {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
