# Patched upstream ANSI dependency

This directory contains `github.com/charmbracelet/x/ansi` v0.11.8 under its
original MIT license. It remains a separate module with its upstream import
path; the runtime's `go.mod` selects this copy through a local replacement.

Runtime PR [#786](https://github.com/wippyai/runtime/pull/786) patched OSC/DCS
parsing so a UTF-8 continuation byte equal to `0x9C` is not mistaken for the
8-bit String Terminator. A standalone `0x9C` still terminates the string.
`parser_osc_utf8_test.go` covers this distinction.

The dependency is shared by the terminal service, TTY system and Lua TTY
module. It is not a first-party Wippy service or API. Keep its upstream module
identity and license when updating it. Once an upstream version passes the
regression tests without the local patch, remove this copy and the replacement.

`make test` runs this nested module's tests separately, since Go's root-module
package patterns do not include nested modules.
