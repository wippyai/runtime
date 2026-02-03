# Demo Extension

Build:

```bash
go build -buildmode=plugin -o demo.so ./extensions/demo
```

Config (`.wippy.yaml`):

```yaml
version: "1.0"
extensions:
  paths:
    - ./demo.so
```
