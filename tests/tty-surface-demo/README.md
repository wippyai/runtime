# TTY surface integration proof

This intentionally small application exercises the complete public path:

```text
physical TTY → shell surface → virtual viewport → child process → PTY proxy
```

The shell owns placement and input routing. The child only sees its granted
terminal and runs a real `/bin/sh` through the `terminal` adapter.

```bash
make build-wippy-linux-amd64
cd tests/tty-surface-demo
../../dist/wippy-linux-amd64 run tty-proof
```

Type in the shell normally. Press `Ctrl+Q` to request a graceful child exit.
