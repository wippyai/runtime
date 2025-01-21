package terminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
)

// EchoTerminal is a simple terminal that echoes input back to output
type EchoTerminal struct {
	prompt string
}

// NewEchoTerminal creates a new echo terminal instance
func NewEchoTerminal(prompt string) *EchoTerminal {
	if prompt == "" {
		prompt = "> "
	}
	return &EchoTerminal{
		prompt: prompt,
	}
}

// Run implements Options interface
func (t *EchoTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)

	// Write initial prompt
	if _, err := fmt.Fprint(out, t.prompt); err != nil {
		return fmt.Errorf("failed to write prompt: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("scanner error: %w", err)
				}
				return nil // EOF
			}

			line := scanner.Text()

			// Echo the input
			if _, err := fmt.Fprintln(out, line); err != nil {
				return fmt.Errorf("failed to write output: %w", err)
			}

			// Write prompt for next line
			if _, err := fmt.Fprint(out, t.prompt); err != nil {
				return fmt.Errorf("failed to write prompt: %w", err)
			}
		}
	}
}
