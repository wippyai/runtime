#!/bin/bash

# Script for wippy to download packcli binaries from GitHub
# Usage: ./download-packcli.sh [version] [platform]

set -e

# Configuration
GITHUB_REPO="wippyai/runtime"
PACKCLI_VERSION=${1:-"latest"}
PLATFORM=${2:-"auto"}

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

# Download all PackCLI assets from the release
echo "📥 Downloading all PackCLI assets from release $PACKCLI_VERSION..."

# Get all assets from the release
ASSETS=$(curl -s -H "Authorization: token $GITHUB_TOKEN" "https://api.github.com/repos/$GITHUB_REPO/releases/tags/$PACKCLI_VERSION" | jq -r '.assets[] | select(.name | test("packcli")) | [.name, .url] | @tsv')

if [ -z "$ASSETS" ]; then
    echo -e "${RED}❌ No PackCLI assets found in release $PACKCLI_VERSION${NC}"
    exit 1
fi

# Download each asset
echo "$ASSETS" | while IFS=$'\t' read -r asset_name asset_url; do
    echo "📥 Downloading $asset_name..."
    if curl -L -f -H "Authorization: token $GITHUB_TOKEN" -H "Accept: application/octet-stream" "$asset_url" -o "$asset_name"; then
        echo "✅ Downloaded $asset_name"
        # Make executable (except for Windows .exe files)
        if [[ "$asset_name" != *.exe ]]; then
            chmod +x "$asset_name"
        fi
    else
        echo -e "${RED}❌ Failed to download $asset_name${NC}"
    fi
done

echo -e "${GREEN}✅ Successfully downloaded all PackCLI assets from $PACKCLI_VERSION${NC}"
echo "📁 Downloaded files:"
ls -la packcli-* 2>/dev/null || echo "No PackCLI files found"

# Test one of the binaries (prefer linux-amd64 if available)
TEST_BINARY=""
if [ -f "packcli-linux-amd64" ]; then
    TEST_BINARY="packcli-linux-amd64"
elif [ -f "packcli-darwin-amd64" ]; then
    TEST_BINARY="packcli-darwin-amd64"
elif [ -f "packcli-windows-amd64.exe" ]; then
    TEST_BINARY="packcli-windows-amd64.exe"
else
    # Find any packcli binary
    TEST_BINARY=$(ls packcli-* 2>/dev/null | head -1)
fi

if [ -n "$TEST_BINARY" ]; then
    echo "🧪 Testing binary: $TEST_BINARY"
    if [[ "$TEST_BINARY" == *.exe ]]; then
        ./"$TEST_BINARY" --version
    else
        ./"$TEST_BINARY" --version
    fi
fi

echo ""
echo -e "${GREEN}🎉 PackCLI is ready for wippy integration!${NC}"
echo ""
echo "Next steps:"
echo "  1. Include all PackCLI binaries in your wippy release assets"
echo "  2. Update your wippy documentation with PackCLI usage"
echo "  3. Clean up: rm packcli-*"
