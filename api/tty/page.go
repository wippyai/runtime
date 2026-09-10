// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// Page supplies the two terminal-default colors for a virtual viewport.
// Both colors must be opaque #RRGGBB values; nil means no page.
type Page struct {
	Foreground string
	Background string
}

func (p Page) Canonical() (Page, error) {
	for _, value := range []string{p.Foreground, p.Background} {
		if len(value) != 7 || value[0] != '#' {
			return Page{}, fmt.Errorf("viewport page requires foreground and background in #RRGGBB form")
		}
		if _, err := strconv.ParseUint(value[1:], 16, 24); err != nil {
			return Page{}, fmt.Errorf("viewport page color must be #RRGGBB: %w", err)
		}
	}
	return Page{Foreground: strings.ToLower(p.Foreground), Background: strings.ToLower(p.Background)}, nil
}

// Colors returns opaque RGB colors from a previously validated page.
func (p Page) Colors() (color.Color, color.Color) {
	parse := func(value string) color.Color {
		n, _ := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 24)
		return color.RGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 255}
	}
	return parse(p.Foreground), parse(p.Background)
}

// PageViewport is an optional owner-side capability. Page policy cannot be
// changed by a delegated mount or by the program painting into the viewport.
type PageViewport interface {
	SetPage(context.Context, *Page) error
}

// PageProvider lets a PTY proxy answer program color queries from its owning
// surface. The value is a copy; false retains the legacy terminal behavior.
type PageProvider interface {
	Page() (Page, bool)
}
