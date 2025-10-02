# macOS Code Signing - Quick Start

## Quick start for macOS binary signing in GitHub CI/CD

### 1. Prepare Certificate

```bash
# Run the automatic setup script
chmod +x scripts/setup-macos-signing.sh
./scripts/setup-macos-signing.sh
```

### 2. Add GitHub Secrets

Go to: `https://github.com/YOUR_USERNAME/YOUR_REPO/settings/secrets/actions`

Add the following secrets:

| Secret | Description |
|--------|-------------|
| `MACOS_CERTIFICATE_P12` | P12 certificate in base64 (from script) |
| `MACOS_CERTIFICATE_PASSWORD` | Password for P12 file |
| `MACOS_CERTIFICATE_NAME` | Full certificate name |
| `MACOS_APPLE_ID` | Your Apple ID |
| `MACOS_APPLE_ID_PASSWORD` | Apple ID password or app-specific password |
| `MACOS_TEAM_ID` | Your Team ID (10 characters) |

### 3. Run CI/CD

Signing happens automatically on:
- Tag push (e.g.: `v1.0.0`)
- Manual workflow trigger via GitHub Actions

### 4. Check Results

After successful execution:
- macOS binaries will be signed with entitlements and notarized
- They will work without Gatekeeper warnings
- Release will be created in `wippyai/wippy-releases`

**Note**: The signing process includes entitlements from `.github/entitlements/macos.entitlements` for proper functionality.

### Local Testing

```bash
# Build with signing
make build-sign-notarize-macos \
  MACOS_CERTIFICATE_NAME="Developer ID Application: Your Company (TEAM_ID)" \
  MACOS_CERTIFICATE_PASSWORD="password" \
  MACOS_APPLE_ID="your@email.com" \
  MACOS_APPLE_ID_PASSWORD="password" \
  MACOS_TEAM_ID="TEAM_ID"

# Verify signature and entitlements
codesign --verify --verbose ./dist/runner-darwin-amd64
codesign --display --entitlements - ./dist/runner-darwin-amd64
spctl --assess --verbose ./dist/runner-darwin-amd64
```

### Troubleshooting

1. **"No identity found"** → Check certificate name
2. **"Invalid certificate"** → Check certificate expiration date  
3. **"Notarization failed"** → Check Apple ID and Team ID

Detailed documentation: [macos-code-signing.md](.github/docs/macos-code-signing.md)
