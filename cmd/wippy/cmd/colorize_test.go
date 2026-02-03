package cmd

import (
	"strings"
	"testing"
)

func TestColorizeTypeError(t *testing.T) {
	input := `error[E0002]: cannot assign string to number
 --> test.lua:5:12
  |
5 | local x: number = "hello"
  |          ------ ^~~~~~~~ expected ` + "`number`" + `, found ` + "`string`" + `
  |          |
  |          expected due to this
  = note: expected ` + "`number`" + `
             found ` + "`string`" + `
`

	result := ColorizeTypeError(input)

	// Should contain ANSI codes
	if !strings.Contains(result, "\033[") {
		t.Error("expected ANSI color codes in output")
	}

	// Should still contain the original text
	if !strings.Contains(result, "cannot assign string to number") {
		t.Error("expected message to be preserved")
	}

	// Should colorize error header
	if !strings.Contains(result, boldRed+"error") {
		t.Error("expected red error header")
	}
}

func TestColorizePlainError(t *testing.T) {
	input := `error: unexpected token
 --> test.lua:3:5
  |
3 | local @@ = 1
  |       ^~
`

	result := ColorizeTypeError(input)

	if !strings.Contains(result, boldRed+"error") {
		t.Error("expected red error header for plain errors")
	}

	if !strings.Contains(result, boldBlue+"-->") {
		t.Error("expected blue location arrow")
	}
}

func TestColorizeWarning(t *testing.T) {
	input := `warning[E0013]: unreachable code
 --> test.lua:10:5
  |
10 | return nil
   |        ^~~
`

	result := ColorizeTypeError(input)

	if !strings.Contains(result, boldYellow+"warning") {
		t.Error("expected yellow warning header")
	}
}

func TestNonRustStyleError(t *testing.T) {
	input := "some random error message"
	result := ColorizeTypeError(input)

	if result != input {
		t.Error("non-Rust-style errors should be returned unchanged")
	}
}

func TestColorizeEmbeddedError(t *testing.T) {
	input := `failed to load state: type check error in module: error: syntax error: unexpected token
 --> test.lua:2:10
  |
2 |     return @
  |          ^~
`

	result := ColorizeTypeError(input)

	// Prefix should be unchanged
	if !strings.HasPrefix(result, "failed to load state: type check error in module: ") {
		t.Error("prefix should be preserved")
	}

	// Error portion should be colorized
	if !strings.Contains(result, boldRed+"error") {
		t.Error("embedded error header should be colorized red")
	}

	if !strings.Contains(result, boldBlue+"-->") {
		t.Error("embedded location should be colorized blue")
	}
}

func TestColorizeNoteAndHelp(t *testing.T) {
	input := `error[E0001]: type mismatch
  = note: expected ` + "`number`" + `
  = help: process.Options is { trap_links: boolean }
`

	result := ColorizeTypeError(input)

	// Note should use cyan
	if !strings.Contains(result, boldCyan+"= note") {
		t.Error("expected cyan note label")
	}

	// Help should use green
	if !strings.Contains(result, boldGreen+"= help") {
		t.Error("expected green help label")
	}
}
