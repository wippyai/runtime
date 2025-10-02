#!/bin/bash

# macOS Code Signing Setup Script
# This script helps you prepare certificates and secrets for macOS code signing in GitHub Actions

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_header() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

print_header "macOS Code Signing Setup Guide"

echo ""
print_info "This script will help you set up macOS code signing for GitHub Actions."
echo ""
print_warning "Prerequisites:"
echo "1. Apple Developer Account with paid membership"
echo "2. Developer ID Application certificate"
echo "3. Apple ID credentials for notarization"
echo ""

# Check if running on macOS
if [[ "$OSTYPE" != "darwin"* ]]; then
    print_error "This script must be run on macOS to export certificates"
    exit 1
fi

print_header "Step 1: Certificate Information"

echo ""
print_info "First, let's gather information about your Developer ID Application certificate:"
echo ""

# List available certificates
print_info "Available Developer ID Application certificates:"
security find-identity -v -p codesigning | grep "Developer ID Application" || {
    print_error "No Developer ID Application certificates found!"
    print_info "Please create a Developer ID Application certificate in Keychain Access or Xcode"
    exit 1
}

echo ""
read -p "Enter the full name of your Developer ID Application certificate (copy exactly): " CERT_NAME

# Verify certificate exists
if ! security find-identity -v -p codesigning | grep -q "$CERT_NAME"; then
    print_error "Certificate '$CERT_NAME' not found!"
    exit 1
fi

print_success "Certificate found: $CERT_NAME"

print_header "Step 2: Export Certificate"

echo ""
print_info "Now we'll export your certificate to a P12 file:"
echo ""

read -p "Enter a password for the P12 file (you'll need this for GitHub secrets): " P12_PASSWORD

# Export certificate
CERT_FILE="developer_id_certificate.p12"
security export -k login.keychain -t identities -f pkcs12 -P "$P12_PASSWORD" -o "$CERT_FILE" "$CERT_NAME"

if [ -f "$CERT_FILE" ]; then
    print_success "Certificate exported to: $CERT_FILE"
else
    print_error "Failed to export certificate"
    exit 1
fi

print_header "Step 3: Apple ID Information"

echo ""
print_info "For notarization, we need your Apple ID information:"
echo ""

read -p "Enter your Apple ID email: " APPLE_ID
read -p "Enter your Apple ID password (or app-specific password): " APPLE_PASSWORD
read -p "Enter your Apple Developer Team ID (10-character string): " TEAM_ID

print_header "Step 4: GitHub Secrets Setup"

echo ""
print_info "Now add these secrets to your GitHub repository:"
echo ""
print_warning "Go to: https://github.com/YOUR_USERNAME/YOUR_REPO/settings/secrets/actions"
echo ""

echo "Add the following secrets:"
echo ""
echo "1. MACOS_CERTIFICATE_P12"
echo "   Value: $(base64 -i "$CERT_FILE")"
echo ""
echo "2. MACOS_CERTIFICATE_PASSWORD"
echo "   Value: $P12_PASSWORD"
echo ""
echo "3. MACOS_CERTIFICATE_NAME"
echo "   Value: $CERT_NAME"
echo ""
echo "4. MACOS_APPLE_ID"
echo "   Value: $APPLE_ID"
echo ""
echo "5. MACOS_APPLE_ID_PASSWORD"
echo "   Value: $APPLE_PASSWORD"
echo ""
echo "6. MACOS_TEAM_ID"
echo "   Value: $TEAM_ID"
echo ""

print_header "Step 5: Verification"

echo ""
print_info "Let's verify the certificate export:"
echo ""

# Show certificate info
print_info "Certificate details:"
security find-identity -v -p codesigning | grep "$CERT_NAME"

echo ""
print_info "P12 file size: $(ls -lh "$CERT_FILE" | awk '{print $5}')"

print_header "Step 6: Cleanup"

echo ""
read -p "Do you want to keep the P12 file for backup? (y/n): " KEEP_FILE

if [[ "$KEEP_FILE" != "y" && "$KEEP_FILE" != "Y" ]]; then
    rm "$CERT_FILE"
    print_info "P12 file removed"
else
    print_info "P12 file kept as: $CERT_FILE"
fi

print_header "Setup Complete!"

echo ""
print_success "macOS code signing setup is complete!"
echo ""
print_info "Next steps:"
echo "1. Add the secrets to your GitHub repository"
echo "2. Push a tag or trigger a release workflow"
echo "3. Check the GitHub Actions logs for signing status"
echo ""
print_warning "Important notes:"
echo "- Keep your Apple ID credentials secure"
echo "- App-specific passwords are recommended over regular passwords"
echo "- The certificate will be used automatically in CI/CD for macOS builds"
echo ""

print_success "Setup completed successfully! 🎉"
