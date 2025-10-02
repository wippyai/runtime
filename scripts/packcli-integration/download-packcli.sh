#!/bin/bash

# Script for wippy to download packcli binaries from GitHub
# Usage: ./download-packcli.sh [version] [platform]
# Examples:
#   ./download-packcli.sh latest                    # Download all PackCLI binaries
#   ./download-packcli.sh latest linux-amd64       # Download only linux-amd64 PackCLI
#   ./download-packcli.sh v0.0.7-alpha7 windows-amd64  # Download specific version for specific platform

set -e

# Configuration
GITHUB_REPO="wippyai/runtime"
PACKCLI_VERSION=${1:-"latest"}
PLATFORM_FILTER=${2:-""}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🔧 PackCLI Downloader for Wippy${NC}"
echo "=================================="

echo "📦 Version: $PACKCLI_VERSION"

# Get latest version if needed
if [ "$PACKCLI_VERSION" = "latest" ]; then
    echo "🔍 Getting latest PackCLI version from GitHub API..."
    # Get the most recent PackCLI release by creation date
    LATEST_VERSION=$(curl -s -H "Authorization: token $GITHUB_TOKEN" "https://api.github.com/repos/$GITHUB_REPO/releases" | jq -r '.[] | select(.name | test("PackCLI")) | [.created_at, .tag_name] | @tsv' | sort -r | head -1 | cut -f2)
    if [ "$LATEST_VERSION" = "null" ] || [ -z "$LATEST_VERSION" ]; then
        echo -e "${RED}❌ Failed to get latest PackCLI version from GitHub${NC}"
        echo "   Available PackCLI releases:"
        curl -s -H "Authorization: token $GITHUB_TOKEN" "https://api.github.com/repos/$GITHUB_REPO/releases" | jq -r '.[] | select(.name | test("PackCLI")) | .created_at + " - " + .name + " (" + .tag_name + ")"' | head -5
        exit 1
    fi
    PACKCLI_VERSION="$LATEST_VERSION"
    echo "✅ Latest PackCLI version: $PACKCLI_VERSION"
fi

# We'll download all PackCLI assets from the release

# Download PackCLI assets from the release
if [ -n "$PLATFORM_FILTER" ]; then
    echo "📥 Downloading PackCLI $PLATFORM_FILTER from release $PACKCLI_VERSION..."
else
    echo "📥 Downloading all PackCLI assets from release $PACKCLI_VERSION..."
fi

# Get assets from the release with optional platform filter
if [ -n "$PLATFORM_FILTER" ]; then
    ASSETS=$(curl -s -H "Authorization: token $GITHUB_TOKEN" "https://api.github.com/repos/$GITHUB_REPO/releases/tags/$PACKCLI_VERSION" | jq -r ".assets[] | select(.name | test(\"packcli.*$PLATFORM_FILTER\")) | [.name, .url] | @tsv")
else
    ASSETS=$(curl -s -H "Authorization: token $GITHUB_TOKEN" "https://api.github.com/repos/$GITHUB_REPO/releases/tags/$PACKCLI_VERSION" | jq -r '.assets[] | select(.name | test("packcli")) | [.name, .url] | @tsv')
fi

if [ -z "$ASSETS" ]; then
    if [ -n "$PLATFORM_FILTER" ]; then
        echo -e "${RED}❌ No PackCLI assets found for platform $PLATFORM_FILTER in release $PACKCLI_VERSION${NC}"
    else
        echo -e "${RED}❌ No PackCLI assets found in release $PACKCLI_VERSION${NC}"
    fi
    echo "   Available PackCLI releases:"
    curl -s -H "Authorization: token $GITHUB_TOKEN" "https://api.github.com/repos/$GITHUB_REPO/releases" | jq -r '.[] | select(.name | test("PackCLI")) | .created_at + " - " + .name + " (" + .tag_name + ")"' | head -5
    exit 1
fi

# Download each asset
echo "$ASSETS" | while IFS=$'\t' read -r asset_name asset_url; do
    echo "📥 Downloading $asset_name..."
    if curl -L -f -H "Authorization: token $GITHUB_TOKEN" -H "Accept: application/octet-stream" "$asset_url" -o "$asset_name"; then
        echo "✅ Downloaded $asset_name"
        
        # Rename file to short format expected by Makefile
        # Convert packcli-linux-arm64-v0.0.7-alpha7 -> packcli-linux-arm64
        # Convert packcli-windows-amd64-v0.0.7-alpha7.exe -> packcli-windows-amd64.exe
        short_name=$(echo "$asset_name" | sed -E 's/-v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)*(\.[a-z]+)?$/\2/')
        # If the regex didn't match, try a simpler approach
        if [ "$short_name" = "$asset_name" ]; then
            short_name=$(echo "$asset_name" | sed -E 's/-v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)*//')
        fi
        if [ "$short_name" != "$asset_name" ]; then
            mv "$asset_name" "$short_name"
            echo "📝 Renamed $asset_name -> $short_name"
        fi
        
        # Make executable (except for Windows .exe files)
        if [[ "$short_name" != *.exe ]]; then
            chmod +x "$short_name"
        fi
    else
        echo -e "${RED}❌ Failed to download $asset_name${NC}"
    fi
done

echo -e "${GREEN}✅ Successfully downloaded all PackCLI assets from $PACKCLI_VERSION${NC}"
echo "📁 Downloaded files:"
ls -la packcli-* 2>/dev/null || echo "No PackCLI files found"

# Test one of the binaries (only if compatible with current architecture)
TEST_BINARY=""
CURRENT_ARCH=$(uname -m)
CURRENT_OS=$(uname -s | tr '[:upper:]' '[:lower:]')

echo "🔍 Current system: $CURRENT_OS-$CURRENT_ARCH"

# Map current architecture to expected binary names
if [ "$CURRENT_OS" = "linux" ]; then
    if [ "$CURRENT_ARCH" = "x86_64" ] && [ -f "packcli-linux-amd64" ]; then
        TEST_BINARY="packcli-linux-amd64"
    elif [ "$CURRENT_ARCH" = "aarch64" ] && [ -f "packcli-linux-arm64" ]; then
        TEST_BINARY="packcli-linux-arm64"
    fi
elif [ "$CURRENT_OS" = "darwin" ]; then
    if [ "$CURRENT_ARCH" = "x86_64" ] && [ -f "packcli-darwin-amd64" ]; then
        TEST_BINARY="packcli-darwin-amd64"
    elif [ "$CURRENT_ARCH" = "arm64" ] && [ -f "packcli-darwin-arm64" ]; then
        TEST_BINARY="packcli-darwin-arm64"
    fi
fi

if [ -n "$TEST_BINARY" ]; then
    echo "🧪 Testing compatible binary: $TEST_BINARY"
    if [[ "$TEST_BINARY" == *.exe ]]; then
        ./"$TEST_BINARY" --version
    else
        ./"$TEST_BINARY" --version
    fi
else
    echo "ℹ️ No compatible binary found for testing ($CURRENT_OS-$CURRENT_ARCH)"
    echo "   This is normal when downloading binaries for different architectures"
fi

echo ""
echo -e "${GREEN}🎉 PackCLI is ready for wippy integration!${NC}"
echo ""
echo "Next steps:"
echo "  1. Include all PackCLI binaries in your wippy release assets"
echo "  2. Update your wippy documentation with PackCLI usage"
echo "  3. Clean up: rm packcli-*"
