package cmd

import (
	"regexp"
	"strings"
)

// ANSI color codes for terminal output
const (
	reset = "\033[0m"
	bold  = "\033[1m"
	cyan  = "\033[36m"

	boldRed    = "\033[1;31m"
	boldGreen  = "\033[1;32m"
	boldYellow = "\033[1;33m"
	boldBlue   = "\033[1;34m"
	boldCyan   = "\033[1;36m"
)

var (
	// Header patterns
	headerWithCode = regexp.MustCompile(`^(error|warning|hint)(\[E\d+\])(:)(.*)$`)
	headerPlain    = regexp.MustCompile(`^(error|warning|hint)(:)(.*)$`)

	// Location: --> file:line:col
	locationPattern = regexp.MustCompile(`^(\s*)(-->)(\s*)(.+):(\d+):(\d+)(.*)$`)

	// Line number with code: " 42 | code here"
	lineNumPattern = regexp.MustCompile(`^(\s*)(\d+)(\s*\|\s?)(.*)$`)

	// Pipe only: "    |"
	pipeOnlyPattern = regexp.MustCompile(`^(\s*)(\|)(\s*)$`)

	// Pipe with content: "    | content"
	pipeContentPattern = regexp.MustCompile(`^(\s*)(\|)(\s?)(.+)$`)

	// Note/help lines: "   = note:" or "   = help:"
	noteHelpPattern = regexp.MustCompile(`^(\s*)(=\s*)(note|help)(:)(.*)$`)

	// Underline markers ^~~~
	caretPattern = regexp.MustCompile(`(\^~*)`)

	// Dash underlines ----
	dashPattern = regexp.MustCompile(`(--+)`)

	// Types in backticks `type`
	backtickType = regexp.MustCompile("`([^`]+)`")

	// "expected `X`, found `Y`" pattern
	expectedFoundPattern = regexp.MustCompile(`(expected\s+)` + "`([^`]+)`" + `(,?\s*found\s+)` + "`([^`]+)`")
)

// ColorizeTypeError applies ANSI colors to Rust-style error output.
// It finds embedded Rust-style errors within larger error messages.
func ColorizeTypeError(s string) string {
	// Find where the Rust-style error starts
	idx := findRustErrorStart(s)
	if idx < 0 {
		return s
	}

	// Split into prefix and error portion
	prefix := s[:idx]
	errorPart := s[idx:]

	// Colorize each line of the error portion
	lines := strings.Split(errorPart, "\n")
	var result []string

	for _, line := range lines {
		result = append(result, colorizeLine(line))
	}

	return prefix + strings.Join(result, "\n")
}

// findRustErrorStart finds where a Rust-style error begins in the string.
// Returns -1 if not found.
func findRustErrorStart(s string) int {
	patterns := []string{
		"error[E",
		"error:",
		"warning[E",
		"warning:",
		"hint[E",
		"hint:",
	}

	minIdx := -1
	for _, p := range patterns {
		if idx := strings.Index(s, p); idx >= 0 {
			if minIdx < 0 || idx < minIdx {
				minIdx = idx
			}
		}
	}
	return minIdx
}

func colorizeLine(line string) string {
	// Header with error code: error[E0001]: message
	if m := headerWithCode.FindStringSubmatch(line); m != nil {
		color := severityColor(m[1])
		return color + m[1] + "[" + m[2][1:len(m[2])-1] + "]" + reset + bold + m[3] + m[4] + reset
	}

	// Header plain: error: message
	if m := headerPlain.FindStringSubmatch(line); m != nil {
		color := severityColor(m[1])
		return color + m[1] + reset + bold + m[2] + m[3] + reset
	}

	// Location: --> file:line:col
	if m := locationPattern.FindStringSubmatch(line); m != nil {
		return m[1] + boldBlue + m[2] + reset + m[3] +
			m[4] + ":" + boldBlue + m[5] + reset + ":" + boldBlue + m[6] + reset + m[7]
	}

	// Line number: " 42 | code"
	if m := lineNumPattern.FindStringSubmatch(line); m != nil {
		return boldBlue + m[1] + m[2] + m[3] + reset + m[4]
	}

	// Pipe only: "    |"
	if m := pipeOnlyPattern.FindStringSubmatch(line); m != nil {
		return boldBlue + m[1] + m[2] + reset + m[3]
	}

	// Pipe with content (underlines, annotations)
	if m := pipeContentPattern.FindStringSubmatch(line); m != nil {
		content := m[4]

		// Colorize caret underlines ^~~~
		content = caretPattern.ReplaceAllStringFunc(content, func(s string) string {
			return boldRed + s + reset
		})

		// Colorize dash underlines ----
		content = dashPattern.ReplaceAllStringFunc(content, func(s string) string {
			return boldBlue + s + reset
		})

		// Colorize expected/found pattern
		content = expectedFoundPattern.ReplaceAllStringFunc(content, func(s string) string {
			m := expectedFoundPattern.FindStringSubmatch(s)
			if m == nil {
				return s
			}
			return m[1] + "`" + boldCyan + m[2] + reset + "`" +
				m[3] + "`" + boldYellow + m[4] + reset + "`"
		})

		// Colorize remaining backticked types
		content = backtickType.ReplaceAllStringFunc(content, func(s string) string {
			inner := s[1 : len(s)-1]
			return "`" + boldCyan + inner + reset + "`"
		})

		// Check for "expected due to this"
		content = strings.ReplaceAll(content, "expected due to this", cyan+"expected due to this"+reset)

		return boldBlue + m[1] + m[2] + reset + m[3] + content
	}

	// Note/help lines: "   = note: ..." or "   = help: ..."
	if m := noteHelpPattern.FindStringSubmatch(line); m != nil {
		labelColor := boldCyan
		if m[3] == "help" {
			labelColor = boldGreen
		}

		content := m[5]
		// Colorize types in backticks
		content = backtickType.ReplaceAllStringFunc(content, func(s string) string {
			inner := s[1 : len(s)-1]
			return "`" + boldCyan + inner + reset + "`"
		})

		return m[1] + labelColor + m[2] + m[3] + reset + m[4] + content
	}

	return line
}

func severityColor(severity string) string {
	switch severity {
	case "error":
		return boldRed
	case "warning":
		return boldYellow
	case "hint":
		return boldCyan
	default:
		return bold
	}
}
