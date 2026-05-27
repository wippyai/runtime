// SPDX-License-Identifier: MPL-2.0

// Package nettest provides helpers for overlay-network driver E2E tests:
// external-IP probing, port parsing, and string utilities that avoid
// pulling the strings package into tight test loops. Each driver
// subpackage contributes its own skipIf* and address resolvers locally.
package nettest

import (
	"context"
	"encoding/json"
	"io"
	gohttp "net/http"
	"testing"
	"time"
)

// GetExternalIP queries public IP-echo services through the provided HTTP
// client and returns the observed external IP. Used by E2E tests to verify
// that outbound traffic egressed through an overlay network. Returns an
// empty string if none of the services could be reached.
func GetExternalIP(t *testing.T, client *gohttp.Client) string {
	t.Helper()

	services := []struct {
		parse func([]byte) string
		url   string
	}{
		{
			url: "https://api.ipify.org?format=json",
			parse: func(body []byte) string {
				var r struct {
					IP string `json:"ip"`
				}
				if json.Unmarshal(body, &r) == nil {
					return r.IP
				}
				return ""
			},
		},
		{
			url: "https://httpbin.org/ip",
			parse: func(body []byte) string {
				var r struct {
					Origin string `json:"origin"`
				}
				if json.Unmarshal(body, &r) == nil {
					return r.Origin
				}
				return ""
			},
		},
	}

	for _, svc := range services {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := gohttp.NewRequestWithContext(ctx, "GET", svc.url, nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := client.Do(req)
		cancel()
		if err != nil {
			t.Logf("IP service %s failed: %v", svc.url, err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		if ip := svc.parse(body); ip != "" {
			return ip
		}
	}

	t.Log("Warning: could not determine external IP from any service")
	return ""
}

// ParsePort parses a decimal port string. Returns (port, true) when the
// string is purely numeric and in the 1..65535 range, (0, false) otherwise.
// Avoids strconv to stay dependency-free.
func ParsePort(s string) (int, bool) {
	port := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		port = port*10 + int(c-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// PortToString renders a port as its decimal representation. Zero and
// negative ports return "0".
func PortToString(port int) string {
	if port <= 0 {
		return "0"
	}
	s := ""
	for port > 0 {
		s = string(rune('0'+port%10)) + s
		port /= 10
	}
	return s
}

// LastNonEmptyLine returns the last non-empty line of a multi-line string
// after trimming whitespace. Used to pick up command output written after
// a stream of log lines (e.g. headscale preauthkey creation).
func LastNonEmptyLine(s string) string {
	lines := splitLines(s)
	for i := len(lines) - 1; i >= 0; i-- {
		line := trimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

// Contains reports whether substr is a substring of s. Avoids the strings
// package to keep test helpers dependency-free.
func Contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ContainsAny reports whether any of substrs is a substring of s.
func ContainsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if Contains(s, sub) {
			return true
		}
	}
	return false
}

// ErrorAs walks the error chain looking for a match that As can extract.
// Equivalent to errors.As without panicking on non-pointer targets.
func ErrorAs(err error, target any) bool {
	type asInterface interface {
		As(any) bool
	}
	for err != nil {
		if x, ok := err.(asInterface); ok {
			if x.As(target) {
				return true
			}
		}
		type unwrapper interface {
			Unwrap() error
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
