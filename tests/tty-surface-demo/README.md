# TTY surface integration proof

This intentionally small application exercises the complete public path:

```text
physical TTY → shell surface → virtual viewport → child process → PTY proxy
```

The shell owns placement and input routing. The child only sees its granted
terminal and runs Bash through the `terminal` adapter. Bash is used deliberately
because minimal `/bin/sh` implementations such as Dash do not provide
interactive line editing: arrow and Home keys would be echoed as escape
sequences even when the terminal transport is correct.

```bash
make build-wippy-linux-amd64
cd tests/tty-surface-demo
../../dist/wippy-linux-amd64 run tty-proof
```

Type in the shell normally. Press `Ctrl+Q` to request a graceful child exit.
