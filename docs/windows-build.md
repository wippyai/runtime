# Windows Build Instructions

This document describes how to build Windows binaries for the Wippy runtime.

## Prerequisites

### For Cross-Compilation (Linux/macOS to Windows)

Install the required toolchain:

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y gcc-mingw-w64-x86-64 gcc-mingw-w64-i686

# macOS (using Homebrew)
brew install mingw-w64
```

### For Native Windows Build

Use MSYS2 with the following packages:
- `mingw-w64-x86_64-toolchain`
- `mingw-w64-x86_64-sqlite3`

## Building Windows Binaries

### Using Makefile

```bash
# Build Windows AMD64
make build-runner-windows-amd64

# Build Windows ARM64
make build-runner-windows-arm64

# Build all cross-platform binaries (including Windows)
make build-runner-cross
```

### Manual Build

```bash
# Windows AMD64
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build --tags "fts5 sqlite_vec" -o "./dist/runner-windows-amd64.exe" "./cmd/runner/"

# Windows ARM64
CGO_ENABLED=1 GOOS=windows GOARCH=arm64 CC=aarch64-w64-mingw32-gcc go build --tags "fts5 sqlite_vec" -o "./dist/runner-windows-arm64.exe" "./cmd/runner/"
```

## GitHub Actions

The project includes GitHub Actions workflows that automatically build Windows binaries:

- **Tests Workflow** (`.github/workflows/tests.yml`): Builds Windows binaries on every push/PR
- **Release Workflow** (`.github/workflows/release.yml`): Creates GitHub releases with Windows binaries

### Release Process

1. Create a git tag: `git tag v1.0.0`
2. Push the tag: `git push origin v1.0.0`
3. The release workflow will automatically:
   - Build Windows AMD64 and ARM64 binaries
   - Create a GitHub release
   - Upload binaries with checksums

### Manual Release

You can also trigger a manual release using the GitHub Actions UI:
1. Go to Actions → Release
2. Click "Run workflow"
3. Enter the desired tag name

## Output Files

The build process creates the following files in the `./dist/` directory:

- `runner-windows-amd64.exe` - Windows AMD64 binary
- `runner-windows-arm64.exe` - Windows ARM64 binary

These are renamed to `wippy-windows-amd64.exe` and `wippy-windows-arm64.exe` in the release process.

## Testing

Use the provided test script to verify Windows builds:

```bash
./scripts/test-windows-build.sh
```

This script will:
1. Build both Windows architectures
2. Verify the binaries were created
3. Show file sizes and confirmations

## Troubleshooting

### Common Issues

1. **Missing toolchain**: Install the required mingw-w64 packages
2. **CGO errors**: Ensure CGO_ENABLED=1 and proper CC environment variable
3. **SQLite issues**: Make sure sqlite3 development libraries are available

### Cross-Compilation Notes

- Windows cross-compilation requires the mingw-w64 toolchain
- ARM64 Windows builds may not work on all systems
- The GitHub Actions runners have the necessary toolchains pre-installed

## Dependencies

The Windows build requires:
- Go 1.24+
- CGO support
- SQLite3 with FTS5 and vector extensions
- mingw-w64 toolchain for cross-compilation
