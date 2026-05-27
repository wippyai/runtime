// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"
)

// RetryConfig is the client-side retry policy used by the hub-mediated
// upload/download/manifest paths. It mirrors the hub's storage.RetryConfig
// so users see consistent timing on both sides; the CLI bumps MaxAttempts
// since a human is waiting and would rather wait through a flaky network
// than retype the command.
type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
	Jitter         time.Duration
}

// DefaultRetryConfig: 8 attempts × backoff up to 30s covers the longest
// realistic blip from any client network (corp proxy / AV interception /
// flaky WiFi) without dragging the user through a hanging command forever.
// Total worst-case wait ≈ 0.5+1+2+4+8+16+30+30 ≈ 91s.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    8,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		Jitter:         250 * time.Millisecond,
	}
}

// Do runs op with jittered exponential backoff. It returns nil on the first
// successful attempt, a wrapped final error after MaxAttempts, or the
// context error on cancellation. Non-retryable errors short-circuit
// immediately so we don't spin on a permanent fault.
func retryDo(ctx context.Context, cfg RetryConfig, op func(attempt int) error) error {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	backoff := cfg.InitialBackoff
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := op(attempt)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
		if attempt == cfg.MaxAttempts {
			break
		}
		sleep := backoff
		if cfg.Jitter > 0 {
			sleep += time.Duration(rand.Int64N(int64(cfg.Jitter) + 1)) //nolint:gosec // jitter only; not security-sensitive
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff = time.Duration(float64(backoff) * cfg.BackoffFactor)
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}
	return lastErr
}

// isRetryable mirrors the hub-side classification — but for client-side
// errors (HTTP status code lives in our typed errors, not in a minio
// ErrorResponse). Network resets, EOF mid-stream, timeouts, HTTP 429, and
// HTTP 5xx are retried; 4xx (other than 429) and context-cancel are not.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		// Windows winsock variants — same wire-level cause, OS-specific text.
		strings.Contains(msg, "wsarecv: ") ||
		strings.Contains(msg, "wsasend: ") {
		return true
	}
	var statusErr *hubStatusError
	if errors.As(err, &statusErr) {
		code := statusErr.statusCode
		return code == http.StatusRequestTimeout ||
			code == http.StatusTooManyRequests ||
			(code >= http.StatusInternalServerError && code < 600)
	}
	return false
}

// hubStatusError captures a non-2xx response from a hub-mediated endpoint
// so the retry classifier can inspect the status code without parsing
// strings. The body is bounded (maxResponseSize) on the read site.
type hubStatusError struct {
	body       string
	statusCode int
}

func (e *hubStatusError) Error() string {
	return httpStatusText(e.statusCode) + ": " + e.body
}

func httpStatusText(code int) string {
	if t := http.StatusText(code); t != "" {
		return "hub responded " + t
	}
	return "hub responded with HTTP status"
}

// IsHubEndpointMissing reports whether err means the hub-mediated route
// itself is absent (an older hub), so the CLI may fall back to the legacy
// presigned-URL flow. A 404 that carries the hub's structured error
// envelope ({"error":{"code":...}}) is a real resource error (e.g. the
// module does not exist) — NOT a missing route — and must not trigger the
// misleading legacy fallback. Only a bare/non-JSON 404 (the net/http
// "404 page not found" an older hub returns for an unknown path) counts.
func IsHubEndpointMissing(err error) bool {
	var statusErr *hubStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	switch statusErr.statusCode {
	case http.StatusMethodNotAllowed:
		return true
	case http.StatusNotFound:
		return hubErrorCode(statusErr.body) == ""
	default:
		return false
	}
}

// IsModuleNotFound reports whether err is the hub's 404 for a target that
// does not exist (module/org). The CLI uses this to tell the user to
// create the module (wippy publish --create) instead of surfacing an
// opaque "resource not found".
func IsModuleNotFound(err error) bool {
	var statusErr *hubStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.statusCode == http.StatusNotFound && hubErrorCode(statusErr.body) == "not_found"
}

// hubErrorCode returns the hub error-envelope code ({"error":{"code"}}),
// or "" when the body is not a structured hub error.
func hubErrorCode(body string) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &e) != nil {
		return ""
	}
	return e.Error.Code
}
