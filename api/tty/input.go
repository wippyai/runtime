// SPDX-License-Identifier: MPL-2.0

package tty

// InputController manages terminal input event reading.
type InputController interface {
	Start() error
	Stop() error
	ScreenSize() (cols int, rows int, err error)
	EnableMouse()
	DisableMouse()
}
