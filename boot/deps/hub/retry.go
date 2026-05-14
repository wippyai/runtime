// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
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

// IsHubEndpointMissing reports whether err is a 404 from a hub-mediated
// endpoint. Used by the CLI to decide whether to fall back to the legacy
// presigned-URL flow when targeting an older hub deployment.
func IsHubEndpointMissing(err error) bool {
	var statusErr *hubStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode == http.StatusNotFound ||
			statusErr.statusCode == http.StatusMethodNotAllowed
	}
	return false
}
