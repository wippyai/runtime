// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"fmt"
	"image/color"

	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/internal/term/vt"
)

// Default palette answered to OSC 10/11/12 queries when the surface supplies
// no page.
var (
	defaultForeground = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	defaultBackground = color.RGBA{A: 0xff}
	defaultCursor     = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// installPageHandlers answers dynamic color queries. A surface that provides a
// page is asked at reply time, so a reply never carries a superseded page.
func (p *Proxy) installPageHandlers() {
	provider, _ := p.surface.(ttyapi.PageProvider)
	for _, command := range []int{10, 11, 12} {
		p.screen.RegisterOscHandler(command, vt.NewOscStringHandler(func(data string) bool {
			if data != "?" {
				return false
			}
			fg, bg, cursor := color.Color(defaultForeground), color.Color(defaultBackground), color.Color(defaultCursor)
			if provider != nil {
				if page, present := provider.Page(); present {
					fg, bg = page.Colors()
				}
			}
			value := map[int]color.Color{10: fg, 11: bg, 12: cursor}[command]
			r, g, b, _ := value.RGBA()
			p.reply(fmt.Sprintf("\x1b]%d;rgb:%04x/%04x/%04x\x07", command, r, g, b))
			return true
		}))
	}
}
