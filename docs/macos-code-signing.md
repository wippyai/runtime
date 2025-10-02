# macOS Code Signing Setup Guide

This guide will help you set up macOS binary signing and notarization in GitHub CI/CD.

## Process Overview

macOS application signing includes:
1. **Code Signing** - signing the binary with a digital signature
2. **Notarization** - submitting to Apple for security verification
3. **Stapling** - attaching the notarization result to the binary

## Prerequisites

### Apple Developer Account
- Paid Apple Developer Program subscription ($99/year)
- Access to Apple Developer Portal
- Ability to create certificates

### Required Certificates
- **Developer ID Application** - for signing applications distributed outside the App Store
- **Developer ID Installer** (optional) - for signing installation packages

## Step-by-Step Setup

### Step 1: Create Certificate

1. Open **Keychain Access** on macOS
2. Go to **Keychain Access > Certificate Assistant > Request a Certificate From a Certificate Authority**
3. Fill out the form:
   - User Email Address: your Apple ID
   - Common Name: your name or company name
   - CA Email Address: leave empty
   - Request is: Saved to disk
4. Save the `.certSigningRequest` file

### Step 2: Create Certificate in Apple Developer Portal

1. Log in to [Apple Developer Portal](https://developer.apple.com/account/)
2. Go to **Certificates, Identifiers & Profiles > Certificates**
3. Click **+** to create a new certificate
4. Select **Developer ID Application**
5. Upload the `.certSigningRequest` file
6. Download the created `.cer` certificate
7. Double-click the file to install it in Keychain

### Step 3: Export Certificate

Use our script for automatic setup:

```bash
chmod +x scripts/setup-macos-signing.sh
./scripts/setup-macos-signing.sh
```

Or do it manually:

```bash
# Find certificate name
security find-identity -v -p codesigning

# Export to P12 format
security export -k login.keychain -t identities -f pkcs12 -P "your_password" -o certificate.p12 "Developer ID Application: Your Name (TEAM_ID)"

# Convert to base64 for GitHub secrets
base64 -i certificate.p12
```

### Step 4: Configure GitHub Secrets

Add the following secrets to your GitHub repository:

| Secret Name | Description | Example Value |
|-------------|-------------|---------------|
| `MACOS_CERTIFICATE_P12` | P12 certificate in base64 | `MIIK...` (long string) |
| `MACOS_CERTIFICATE_PASSWORD` | Password for P12 file | `your_secure_password` |
| `MACOS_CERTIFICATE_NAME` | Full certificate name | `Developer ID Application: Your Company (TEAM_ID)` |
| `MACOS_APPLE_ID` | Apple ID for notarization | `your@email.com` |
| `MACOS_APPLE_ID_PASSWORD` | Apple ID password | `your_password` or app-specific password |
| `MACOS_TEAM_ID` | Team ID from Apple Developer | `ABCD123456` |

### Step 5: App-Specific Password (recommended)

For security, use app-specific password instead of regular password:

1. Go to [Apple ID settings](https://appleid.apple.com/)
2. Log in
3. In **Security** section, find **App-Specific Passwords**
4. Create a new password for "GitHub Actions"
5. Use this password in `MACOS_APPLE_ID_PASSWORD`

## How It Works in CI/CD

### Automatic Process

1. **Certificate Import**: CI/CD creates a temporary keychain and imports the certificate
2. **Build**: macOS binary is built
3. **Signing**: Binary is signed using `codesign`
4. **Notarization**: Binary is submitted to Apple for verification
5. **Stapling**: Notarization result is attached to the binary
6. **Cleanup**: Temporary keychain is removed

### Makefile Commands

```bash
# Signing only
make sign-macos-binary MACOS_CERTIFICATE_NAME="Developer ID Application: Your Company (TEAM_ID)" MACOS_CERTIFICATE_PASSWORD="password"

# Notarization only
make notarize-macos-binary MACOS_APPLE_ID="your@email.com" MACOS_APPLE_ID_PASSWORD="password" MACOS_TEAM_ID="TEAM_ID"

# Full process: build + sign + notarize
make build-sign-notarize-macos
```

## Signature Verification

### Local Verification

```bash
# Verify signature
codesign --verify --verbose ./dist/runner-darwin-amd64

# Verify notarization
spctl --assess --verbose ./dist/runner-darwin-amd64

# Detailed signature information
codesign --display --verbose=4 ./dist/runner-darwin-amd64
```

### CI/CD Verification

CI/CD automatically verifies:
- Signature correctness
- Notarization status
- Gatekeeper compatibility

## Troubleshooting

### Common Errors

1. **"No identity found"**
   - Check certificate name correctness
   - Ensure certificate is installed in keychain

2. **"Invalid certificate"**
   - Check certificate expiration date
   - Ensure correct certificate type is used

3. **"Notarization failed"**
   - Check Apple ID and password
   - Ensure Team ID is correct
   - Check status in [Apple Developer Portal](https://developer.apple.com/notarization/)

### Logs and Debugging

```bash
# Enable verbose logs
export CODESIGN_VERBOSE=1

# Check notarization status
xcrun notarytool history --apple-id "your@email.com" --password "password" --team-id "TEAM_ID"
```

## Security

### Recommendations

1. **Use app-specific passwords** instead of regular passwords
2. **Regularly update certificates** (they expire in 1 year)
3. **Limit access** to GitHub secrets
4. **Monitor usage** of certificates

### Certificate Rotation

- Certificates are valid for 1 year
- Create new certificates 30 days before expiration
- Update GitHub secrets with new certificates

## Additional Resources

- [Apple Code Signing Guide](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/)
- [Notarization Guide](https://developer.apple.com/documentation/security/notarizing_macos_software_before_distribution)
- [GitHub Actions macOS Runner](https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners#supported-runners-and-hardware-resources)
