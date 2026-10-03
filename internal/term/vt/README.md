# internal/term/vt

Headless VT/ANSI terminal emulator, vendored from
https://github.com/gitpod-io/xterm-go at commit
`dae5128cb6b377a559b07d4a2d9eb1321f05e390` (MIT, see LICENSE), a pure-Go port
of xterm.js.

The package is renamed from `xterm` to `vt` and is maintained in-tree from this
commit. `conformance/testdata/golden.json` is the upstream xterm.js golden data
used by `conformance_test.go`.
