// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"bytes"
	"io"

	"github.com/charmbracelet/x/ansi"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func (p *Proxy) installPageHandlers() {
	provider, ok := p.surface.(ttyapi.PageProvider)
	if !ok {
		return
	}
	for _, command := range []int{10, 11} {
		p.screen.RegisterOscHandler(command, func(data []byte) bool {
			parts := bytes.Split(data, []byte{';'})
			if len(parts) != 2 || !bytes.Equal(parts[1], []byte{'?'}) {
				return false
			}
			page, present := provider.Page()
			if !present {
				return false
			}
			fg, bg := page.Colors()
			response := ansi.SetForegroundColor(ansi.XRGBColor{Color: fg}.String())
			if command == 11 {
				response = ansi.SetBackgroundColor(ansi.XRGBColor{Color: bg}.String())
			}
			_, _ = io.WriteString(p.screen.InputPipe(), response)
			return true
		})
	}
}
