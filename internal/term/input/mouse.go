// SPDX-License-Identifier: MPL-2.0

package input

const x10Offset = 32

// mouseButton decodes the button byte shared by the X10 and SGR protocols.
func mouseButton(b int) (mod Mod, btn MouseButton, release, motion bool) {
	const (
		bitShift  = 0b0000_0100
		bitAlt    = 0b0000_1000
		bitCtrl   = 0b0001_0000
		bitMotion = 0b0010_0000
		bitWheel  = 0b0100_0000
		bitExtra  = 0b1000_0000
		bitsMask  = 0b0000_0011
	)
	if b&bitAlt != 0 {
		mod |= ModAlt
	}
	if b&bitCtrl != 0 {
		mod |= ModCtrl
	}
	if b&bitShift != 0 {
		mod |= ModShift
	}
	switch {
	case b&bitExtra != 0:
		btn = MouseBackward + MouseButton(b&bitsMask)
	case b&bitWheel != 0:
		btn = MouseWheelUp + MouseButton(b&bitsMask)
	default:
		btn = MouseLeft + MouseButton(b&bitsMask)
		if b&bitsMask == bitsMask {
			btn = MouseNone
			release = true
		}
	}
	motion = b&bitMotion != 0 && !isWheel(btn)
	return mod, btn, release, motion
}

func isWheel(btn MouseButton) bool {
	return btn >= MouseWheelUp && btn <= MouseWheelRight
}

// sgrMouse decodes CSI < button ; x ; y (M|m), with one-based coordinates.
func sgrMouse(c *csi) Event {
	mod, btn, _, motion := mouseButton(c.param(0, 0, 0))
	m := Mouse{X: c.param(1, 0, 1) - 1, Y: c.param(2, 0, 1) - 1, Button: btn, Mod: mod}
	switch {
	case isWheel(btn):
		return MouseWheelEvent(m)
	case motion:
		return MouseMotionEvent(m)
	case c.final == 'm':
		return MouseReleaseEvent(m)
	}
	return MouseClickEvent(m)
}

// x10Mouse decodes the three bytes that follow CSI M.
func x10Mouse(cb, cx, cy byte) Event {
	b := int(cb)
	if b >= x10Offset {
		b -= x10Offset
	}
	mod, btn, release, motion := mouseButton(b)
	m := Mouse{X: int(cx) - x10Offset - 1, Y: int(cy) - x10Offset - 1, Button: btn, Mod: mod}
	switch {
	case isWheel(btn):
		return MouseWheelEvent(m)
	case motion:
		return MouseMotionEvent(m)
	case release:
		return MouseReleaseEvent(m)
	}
	return MouseClickEvent(m)
}
