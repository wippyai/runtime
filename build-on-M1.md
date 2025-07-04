# 🛠️ Building Go Projects on macOS M1 (Apple Silicon)

## ✅ Prerequisites

* macOS with Apple Silicon (M1/M2/M3)
* Terminal (zsh or bash)
* [Rosetta 2](https://support.apple.com/en-us/HT211861):

  ```bash
  softwareupdate --install-rosetta
  ```

---

## 1. 🔧 Install Homebrew (Both Architectures)

### Native M1 Homebrew (ARM64)

/install location: `/opt/homebrew`

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

### Intel Homebrew (x86\_64 under Rosetta)

/install location: `/usr/local`

```bash
arch -x86_64 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

---

## 2. 📦 Install Go and Build Tools

### Native (M1) Go + SQLite

```bash
/opt/homebrew/bin/brew install go gcc sqlite3
```

### Intel (x86\_64) SQLite (for Rosetta CGO builds)

```bash
arch -x86_64 /usr/local/bin/brew install sqlite3
```

---

## 3. 🏗️ Build Commands

### ✅ Native M1 (ARM64)

```bash
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
CC=clang \
go build --tags "fts5 sqlite_vec" -o dist/runner-darwin-arm64 ./cmd/runner/main.go
```

### ✅ Intel Binary on M1 (x86\_64 via Rosetta)

```bash
arch -x86_64 env \
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
CC=clang \
CGO_CFLAGS="-I/usr/local/include" \
CGO_LDFLAGS="-L/usr/local/lib" \
go build --tags "fts5 sqlite_vec" -o dist/runner-darwin-amd64 ./cmd/runner/main.go
```

---

## 4. ✅ Verify Build Architecture

```bash
file dist/runner-darwin-arm64
# Mach-O 64-bit executable arm64

file dist/runner-darwin-amd64
# Mach-O 64-bit executable x86_64
```

---

## 🧠 Notes

* Run Intel builds entirely under Rosetta (`arch -x86_64 env ...`).
* Intel builds require x86\_64 SQLite libraries from `/usr/local`.
* Native ARM64 builds are faster and should be preferred unless you need Intel compatibility.
