#!/bin/bash

# Script for wippy to download packcli binaries from GitHub
# Usage: ./download-packcli.sh [version] [platform]

set -e

# Configuration
GITHUB_REPO="wippyai/wippy-releases"
PACKCLI_VERSION=${1:-"latest"}
PLATFORM=${2:-"auto"}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🔧 PackCLI Downloader for Wippy${NC}"
echo "=================================="

# Auto-detect platform if not specified
if [ "$PLATFORM" = "auto" ]; then
    case "$(uname -s)" in
        Linux*)     PLATFORM="linux-amd64";;
        Darwin*)    PLATFORM="darwin-amd64";;
        CYGWIN*|MINGW*|MSYS*) PLATFORM="windows-amd64";;
        *)          echo -e "${RED}❌ Unsupported platform: $(uname -s)${NC}"; exit 1;;
    esac
fi

echo "📦 Version: $PACKCLI_VERSION"
echo "🖥️  Platform: $PLATFORM"

# Determine binary name
if [ "$PLATFORM" = "windows-amd64" ]; then
    BINARY_NAME="packcli-windows-amd64.exe"
else
    BINARY_NAME="packcli-${PLATFORM}"
fi

# Get latest version if needed
if [ "$PACKCLI_VERSION" = "latest" ]; then
    echo "🔍 Getting latest version from GitHub API..."
    LATEST_VERSION=$(curl -s "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | jq -r '.tag_name')
    if [ "$LATEST_VERSION" = "null" ] || [ -z "$LATEST_VERSION" ]; then
        echo -e "${RED}❌ Failed to get latest version from GitHub${NC}"
        exit 1
    fi
    PACKCLI_VERSION="$LATEST_VERSION"
    echo "✅ Latest version: $PACKCLI_VERSION"
fi

# Construct GitHub URLs
BINARY_URL="https://github.com/$GITHUB_REPO/releases/download/$PACKCLI_VERSION/$BINARY_NAME"

echo "📥 Binary URL: $BINARY_URL"

# Download binary
echo "📥 Downloading $BINARY_NAME..."
if ! curl -L -f "$BINARY_URL" -o "$BINARY_NAME"; then
    echo -e "${RED}❌ Failed to download binary from $BINARY_URL${NC}"
    echo "   Make sure the release exists: https://github.com/$GITHUB_REPO/releases/tag/$PACKCLI_VERSION"
    exit 1
fi

# Make executable (except for Windows)
if [ "$PLATFORM" != "windows-amd64" ]; then
    chmod +x "$BINARY_NAME"
fi

echo -e "${GREEN}✅ Successfully downloaded PackCLI $PACKCLI_VERSION for $PLATFORM${NC}"
echo "📁 Binary: $BINARY_NAME"

# Test the binary
echo "🧪 Testing binary..."
if [ "$PLATFORM" = "windows-amd64" ]; then
    ./"$BINARY_NAME" --version
else
    ./"$BINARY_NAME" --version
fi

echo ""
echo -e "${GREEN}🎉 PackCLI is ready for wippy integration!${NC}"
echo ""
echo "Next steps:"
echo "  1. Include $BINARY_NAME in your wippy release assets"
echo "  2. Update your wippy documentation with PackCLI usage"
echo "  3. Clean up: rm $BINARY_NAME"
