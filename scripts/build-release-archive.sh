#!/bin/bash

# Build release archive script
# Usage: ./scripts/build-release-archive.sh <platform> <arch> <format> <version>
# Example: ./scripts/build-release-archive.sh linux amd64 tar.gz 0.0.14-alpha

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
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

# Check arguments
if [ $# -ne 4 ]; then
    print_error "Usage: $0 <platform> <arch> <format> <version>"
    print_error "Example: $0 linux amd64 tar.gz 0.0.14-alpha"
    exit 1
fi

PLATFORM="$1"
ARCH="$2"
FORMAT="$3"
VERSION="$4"

print_info "Building release archive for $PLATFORM-$ARCH ($FORMAT) version $VERSION"

# Create temporary directory
TEMP_DIR="./dist/release-temp"
mkdir -p "$TEMP_DIR"

# Determine file extensions based on platform
if [ "$PLATFORM" = "windows" ]; then
    RUNNER_EXT=".exe"
    PACKCLI_EXT=".exe"
else
    RUNNER_EXT=""
    PACKCLI_EXT=""
fi

# Function to find and copy PackCLI binary
copy_packcli() {
    local packcli_pattern="packcli-${PLATFORM}-${ARCH}"
    local packcli_file=""
    
    # Look for PackCLI files with version suffix
    for file in ./packcli-${PLATFORM}-${ARCH}*; do
        if [ -f "$file" ]; then
            packcli_file="$file"
            break
        fi
    done
    
    if [ -n "$packcli_file" ]; then
        cp "$packcli_file" "$TEMP_DIR/packcli$PACKCLI_EXT"
        print_success "Copied PackCLI: $packcli_file -> packcli$PACKCLI_EXT"
        return 0
    else
        print_warning "PackCLI binary not found for $PLATFORM-$ARCH"
        return 1
    fi
}

# Function to find and copy runner binary
copy_runner() {
    local runner_file="./dist/runner-${PLATFORM}-${ARCH}${RUNNER_EXT}"
    
    if [ -f "$runner_file" ]; then
        cp "$runner_file" "$TEMP_DIR/wippy$RUNNER_EXT"
        print_success "Copied runner: $runner_file -> wippy$RUNNER_EXT"
        return 0
    else
        print_warning "Runner binary not found for $PLATFORM-$ARCH"
        return 1
    fi
}

# Copy binaries
copy_runner || true  # Don't fail if runner is missing
copy_packcli || true  # Don't fail if PackCLI is missing

# Check if we have at least one binary
if [ ! -f "$TEMP_DIR/wippy$RUNNER_EXT" ] && [ ! -f "$TEMP_DIR/packcli$PACKCLI_EXT" ]; then
    print_error "No binaries found for $PLATFORM-$ARCH"
    rm -rf "$TEMP_DIR"
    exit 1
fi

# For Windows ARM64, PackCLI-only releases are allowed
if [ "$PLATFORM" = "windows" ] && [ "$ARCH" = "arm64" ] && [ ! -f "$TEMP_DIR/wippy$RUNNER_EXT" ] && [ -f "$TEMP_DIR/packcli$PACKCLI_EXT" ]; then
    print_warning "Windows ARM64 PackCLI-only release (no runner binary)"
fi

# Compress binaries with UPX
print_info "Compressing binaries with UPX..."

if [ -f "$TEMP_DIR/wippy$RUNNER_EXT" ]; then
    if [ "$PLATFORM" = "darwin" ]; then
        upx --best --lzma --force-macos "$TEMP_DIR/wippy$RUNNER_EXT" || print_warning "UPX skipped wippy$RUNNER_EXT (file too small or other reason)"
    else
        upx --best --lzma "$TEMP_DIR/wippy$RUNNER_EXT" || print_warning "UPX skipped wippy$RUNNER_EXT (file too small or other reason)"
    fi
    print_success "Processed wippy$RUNNER_EXT"
fi

if [ -f "$TEMP_DIR/packcli$PACKCLI_EXT" ]; then
    if [ "$PLATFORM" = "darwin" ]; then
        upx --best --lzma --force-macos "$TEMP_DIR/packcli$PACKCLI_EXT" || print_warning "UPX skipped packcli$PACKCLI_EXT (file too small or other reason)"
    else
        upx --best --lzma "$TEMP_DIR/packcli$PACKCLI_EXT" || print_warning "UPX skipped packcli$PACKCLI_EXT (file too small or other reason)"
    fi
    print_success "Processed packcli$PACKCLI_EXT"
fi

# Create archive
ARCHIVE_NAME="wippy-${PLATFORM}-${ARCH}-${VERSION}.${FORMAT}"
print_info "Creating archive: $ARCHIVE_NAME"

cd "$TEMP_DIR"

# Build file list for archive
FILES=""
if [ -f "wippy$RUNNER_EXT" ]; then
    FILES="$FILES wippy$RUNNER_EXT"
fi
if [ -f "packcli$PACKCLI_EXT" ]; then
    FILES="$FILES packcli$PACKCLI_EXT"
fi

# Create archive based on format
if [ "$FORMAT" = "tar.gz" ]; then
    tar -czf "../$ARCHIVE_NAME" $FILES
elif [ "$FORMAT" = "zip" ]; then
    zip "../$ARCHIVE_NAME" $FILES
else
    print_error "Unsupported format: $FORMAT"
    cd - > /dev/null
    rm -rf "$TEMP_DIR"
    exit 1
fi

cd - > /dev/null

# Cleanup
rm -rf "$TEMP_DIR"

print_success "Created $ARCHIVE_NAME"
print_info "Archive contents:"
if [ "$FORMAT" = "tar.gz" ]; then
    tar -tzf "./dist/$ARCHIVE_NAME"
elif [ "$FORMAT" = "zip" ]; then
    unzip -l "./dist/$ARCHIVE_NAME"
fi

print_success "Release archive build completed successfully!"
