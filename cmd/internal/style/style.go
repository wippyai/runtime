// Package style renders ANSI SGR colored and bold text for CLI output on stdout.
package style

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/term"
)

type profile int

const (
	profileNone profile = iota
	profile16
	profile256
)

var (
	profileOnce sync.Once
	current     profile
)

func detect() profile {
	if os.Getenv("NO_COLOR") != "" {
		return profileNone
	}
	forced := os.Getenv("CLICOLOR_FORCE") != "" && os.Getenv("CLICOLOR_FORCE") != "0"
	if !forced && !term.IsTerminal(int(os.Stdout.Fd())) {
		return profileNone
	}
	termEnv := os.Getenv("TERM")
	if termEnv == "dumb" {
		return profileNone
	}
	if !enableVirtualTerminal() {
		return profileNone
	}
	colorTerm := os.Getenv("COLORTERM")
	if colorTerm == "truecolor" || colorTerm == "24bit" || strings.Contains(termEnv, "256color") {
		return profile256
	}
	return profile16
}

func activeProfile() profile {
	profileOnce.Do(func() { current = detect() })
	return current
}

// Style is an immutable text style. The zero value renders text unchanged.
type Style struct {
	fg   int
	hasF bool
	bold bool
}

// New returns an empty style.
func New() Style { return Style{} }

// Bold returns a copy with bold set.
func (s Style) Bold(on bool) Style {
	s.bold = on
	return s
}

// Foreground returns a copy with the given 256-color palette index as foreground.
func (s Style) Foreground(color int) Style {
	s.fg = color
	s.hasF = true
	return s
}

// Render wraps every non-empty line of text in the style's escape sequences
// when stdout supports color; otherwise text is returned unchanged apart from
// tab expansion.
func (s Style) Render(text string) string {
	text = strings.ReplaceAll(text, "\t", "    ")
	p := activeProfile()
	if p == profileNone {
		return text
	}
	seq := s.sequence(p)
	if seq == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = "\x1b[" + seq + "m" + line + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

func (s Style) sequence(p profile) string {
	var parts []string
	if s.bold {
		parts = append(parts, "1")
	}
	if s.hasF {
		parts = append(parts, foreground(s.fg, p))
	}
	return strings.Join(parts, ";")
}

func foreground(color int, p profile) string {
	if color >= 16 && p == profile16 {
		color = to16(color)
	}
	switch {
	case color < 8:
		return strconv.Itoa(30 + color)
	case color < 16:
		return strconv.Itoa(90 + color - 8)
	default:
		return "38;5;" + strconv.Itoa(color)
	}
}

// to16 maps a 256-palette index onto the nearest basic ANSI color.
func to16(color int) int {
	var r, g, b int
	switch {
	case color >= 232:
		v := (color-232)*10 + 8
		if v < 128 {
			return 0
		}
		if v < 200 {
			return 7
		}
		return 15
	default:
		c := color - 16
		r, g, b = c/36, (c/6)%6, c%6
	}
	idx := 0
	if r >= 3 {
		idx |= 1
	}
	if g >= 3 {
		idx |= 2
	}
	if b >= 3 {
		idx |= 4
	}
	if r >= 4 || g >= 4 || b >= 4 {
		idx += 8
	}
	return idx
}
