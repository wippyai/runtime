// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"fmt"
	"strings"
	"unicode"
)

// ParseCommand splits a command into an executable and literal arguments.
// It applies shell-like quoting and escaping without expansion or evaluation.
func ParseCommand(command string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	started := false
	escaped := false

	flush := func() {
		if started {
			args = append(args, word.String())
			word.Reset()
			started = false
		}
	}

	for _, current := range command {
		if escaped {
			if commandEscapeConsumes(quote, current) {
				word.WriteRune(current)
			} else {
				word.WriteRune('\\')
				word.WriteRune(current)
			}
			escaped = false
			continue
		}
		if quote == 0 {
			switch {
			case unicode.IsSpace(current):
				flush()
			case current == '\'' || current == '"':
				quote, started = current, true
			case current == '\\':
				started = true
				escaped = true
			default:
				started = true
				word.WriteRune(current)
			}
			continue
		}

		if current == quote {
			quote = 0
			continue
		}
		if quote == '"' && current == '\\' {
			escaped = true
			continue
		}
		word.WriteRune(current)
	}
	if escaped {
		word.WriteRune('\\')
	}

	if quote != 0 {
		return nil, fmt.Errorf("%w: unclosed %c quote", ErrInvalidCommand, quote)
	}
	flush()
	if len(args) == 0 || args[0] == "" {
		return nil, ErrCommandRequired
	}
	return args, nil
}

func commandEscapeConsumes(quote, value rune) bool {
	if quote == '"' {
		return value == '"' || value == '\\'
	}
	return quote == 0 && (unicode.IsSpace(value) || value == '\'' || value == '"' || value == '\\')
}
