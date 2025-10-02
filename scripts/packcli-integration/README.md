# PackCLI Integration for Wippy

This directory contains scripts for integrating PackCLI (Packer CLI) with Wippy releases.

## Overview

PackCLI is a helper tool that doesn't have its own releases. Instead, it's built and made available for Wippy to include in its releases. This integration allows Wippy to automatically download and include PackCLI binaries when creating releases.

## How It Works

1. **PackCLI Build Process** (in `estimation-engine/packer`):
   - When a tag is created, PackCLI builds binaries for all platforms
   - Binaries are uploaded to GitHub releases in the `wippyai/runtime` repository
   - Metadata is created with version information

2. **Wippy Release Process** (in this repository):
   - When Wippy creates a release, it downloads the latest available PackCLI binaries from GitHub releases
   - **No version matching**: Wippy and PackCLI have independent release cycles
   - PackCLI binaries are included as assets in the Wippy release
   - Users get both Wippy runtime and PackCLI helper tool

## Scripts

### 1. `download-packcli.sh` (Primary - GitHub)

Downloads PackCLI binaries from GitHub releases.

```bash
# Download latest version
./download-packcli.sh

# Download specific version
./download-packcli.sh v1.2.3

# Download for specific platform
./download-packcli.sh v1.2.3 linux-amd64
```

**Features:**
- Auto-detects platform (Linux, macOS, Windows)
- Downloads from GitHub releases in `wippyai/runtime` repository
- Includes metadata with build information
- Tests downloaded binary

### 2. `download-packcli-github.sh` (Alternative - GitHub)

Downloads PackCLI binaries from GitHub releases in the `wippyai/wippy-releases` repository.

```bash
# Download latest version
./download-packcli-github.sh

# Download specific version
./download-packcli-github.sh v1.2.3

# Download for specific platform
./download-packcli-github.sh v1.2.3 linux-amd64
```

**Features:**
- Auto-detects platform (Linux, macOS, Windows)
- Downloads from GitHub releases in `wippyai/wippy-releases` repository
- Includes metadata with build information
- Tests downloaded binary

## Integration in Wippy CI/CD

The integration is configured in `.github/workflows/ci-cd.yml`:

```yaml
- name: Download PackCLI binaries
  run: |
    echo "🔧 Downloading latest PackCLI binaries for integration..."
    
    # Use local PackCLI integration script
    chmod +x ./scripts/packcli-integration/download-packcli.sh
    
    # Always download the latest available PackCLI version
    echo "📦 Downloading latest PackCLI version (independent of Wippy version)..."
    
    if ./scripts/packcli-integration/download-packcli.sh "latest"; then
      echo "✅ PackCLI binaries downloaded successfully"
      # Move PackCLI binaries to release directory
      mv packcli-* ./release-binaries/ 2>/dev/null || echo "No PackCLI binaries to move"
      mv packcli-metadata.json ./release-binaries/ 2>/dev/null || echo "No PackCLI metadata to move"
      echo "✅ PackCLI binaries included in Wippy release"
    else
      echo "⚠️  PackCLI binaries not available - Wippy release will be created without PackCLI"
      echo "   This is normal if PackCLI hasn't been built yet"
    fi
```

## Binary Storage

PackCLI binaries are stored in GitHub releases:

### Primary: GitHub Releases
- **Repository**: `wippyai/runtime`
- **Type**: Regular releases
- **URLs**: `https://github.com/wippyai/runtime/releases/download/{version}/packcli-{platform}`

### Alternative: GitHub Releases (Alternative Repository)
- **Repository**: `wippyai/wippy-releases`
- **Type**: Regular releases
- **URLs**: `https://github.com/wippyai/wippy-releases/releases/download/{version}/packcli-{platform}`

## Platform Support

- **Linux AMD64**: `packcli-linux-amd64`
- **Linux ARM64**: `packcli-linux-arm64`
- **macOS AMD64**: `packcli-darwin-amd64`
- **macOS ARM64**: `packcli-darwin-arm64`
- **Windows AMD64**: `packcli-windows-amd64.exe`

## Release Notes Integration

PackCLI is automatically included in Wippy release notes:

- **Downloads section**: Lists all PackCLI binaries
- **Installation section**: Provides usage examples
- **Release summary**: Shows PackCLI binary information

## Troubleshooting

### Common Issues

1. **PackCLI binaries not found**: This is normal for new Wippy versions - PackCLI will be available in future releases
2. **Download script fails**: Check GitHub access and permissions
3. **Binary not executable**: Ensure proper permissions are set

### Debug Mode

Run scripts with debug output:

```bash
bash -x ./scripts/packcli-integration/download-packcli.sh v1.2.3
```

## Development

To test the integration locally:

1. **Build PackCLI** in the packer repository:
   ```bash
   cd /path/to/estimation-engine/packer
   git tag v1.2.3
   git push origin v1.2.3
   ```

2. **Test download** in wippy:
   ```bash
   cd /path/to/runtime
   ./scripts/packcli-integration/download-packcli.sh v1.2.3
   ```

## Support

For issues with PackCLI integration:
- Check the PackCLI repository: `estimation-engine/packer`
- Review the build logs in GitLab CI/CD
- Verify GitHub access and permissions
- Check Wippy release logs in GitHub Actions