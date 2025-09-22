#!/bin/bash

# Script to generate Markdown files with artifact links for GitHub Actions
# Usage: ./generate-artifacts-md.sh [PR_NUMBER] [COMMIT_SHA] [BUILD_DATE] [ARTIFACTS_DIR]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
PR_NUMBER="${1:-}"
COMMIT_SHA="${2:-}"
BUILD_TIMESTAMP="${3:-$(date +%Y-%m-%d-%H-%M)}"
ARTIFACTS_DIR="${4:-./dist}"
RELEASE_REPO="${RELEASE_REPO:-wippyai/wippy-releases}"
GITHUB_REPO="${GITHUB_REPOSITORY:-}"
GITHUB_RUN_ID="${GITHUB_RUN_ID:-}"

# Function to print colored output
log() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Function to get PR number from GitHub context
get_pr_number() {
    if [ -n "${GITHUB_EVENT_NAME:-}" ] && [ "$GITHUB_EVENT_NAME" = "pull_request" ]; then
        if [ -f "${GITHUB_EVENT_PATH:-}" ]; then
            jq -r '.pull_request.number' "$GITHUB_EVENT_PATH" 2>/dev/null || echo ""
        fi
    elif [ -n "${GITHUB_REF:-}" ]; then
        # Extract PR number from ref like refs/pull/123/merge
        echo "$GITHUB_REF" | sed -n 's|refs/pull/\([0-9]*\)/merge|\1|p' || echo ""
    fi
}

# Function to get commit SHA
get_commit_sha() {
    if [ -n "${GITHUB_SHA:-}" ]; then
        echo "$GITHUB_SHA"
    else
        git rev-parse HEAD 2>/dev/null || echo ""
    fi
}

# Function to get short SHA
get_short_sha() {
    local sha="$1"
    echo "${sha:0:7}"
}

# Function to detect platform from filename
detect_platform() {
    local filename="$1"
    case "$filename" in
        *linux-amd64*) echo "Linux (AMD64)" ;;
        *linux-arm64*) echo "Linux (ARM64)" ;;
        *windows-amd64*) echo "Windows (AMD64)" ;;
        *windows-arm64*) echo "Windows (ARM64)" ;;
        *darwin-amd64*) echo "macOS (AMD64)" ;;
        *darwin-arm64*) echo "macOS (ARM64)" ;;
        *) echo "Unknown" ;;
    esac
}

# Function to get file size
get_file_size() {
    local file="$1"
    if [ -f "$file" ]; then
        if command -v stat >/dev/null 2>&1; then
            # Linux/macOS
            stat -c%s "$file" 2>/dev/null || stat -f%z "$file" 2>/dev/null || echo "Unknown"
        else
            echo "Unknown"
        fi
    else
        echo "Unknown"
    fi
}

# Function to format file size
format_file_size() {
    local size="$1"
    if [ "$size" = "Unknown" ] || [ -z "$size" ]; then
        echo "Unknown"
        return
    fi
    
    if [ "$size" -lt 1024 ]; then
        echo "${size}B"
    elif [ "$size" -lt 1048576 ]; then
        echo "$(( size / 1024 ))KB"
    else
        echo "$(( size / 1048576 ))MB"
    fi
}

# Function to generate artifact links
generate_artifact_links() {
    local artifacts_dir="$1"
    local pr_number="$2"
    local commit_sha="$3"
    local build_timestamp="$4"
    
    local links=""
    
    if [ ! -d "$artifacts_dir" ]; then
        warn "Artifacts directory $artifacts_dir does not exist"
        return
    fi
    
    # Find all binary files
    local files=()
    while IFS= read -r -d '' file; do
        files+=("$file")
    done < <(find "$artifacts_dir" -type f \( -name "wippy-*" -o -name "runner-*" \) -print0 2>/dev/null)
    
    if [ ${#files[@]} -eq 0 ]; then
        warn "No binary files found in $artifacts_dir"
        return
    fi
    
    log "Found ${#files[@]} binary files"
    
    # Group by platform
    local platforms=()
    for file in "${files[@]}"; do
        local platform=$(detect_platform "$(basename "$file")")
        if [[ ! " ${platforms[*]} " =~ " ${platform} " ]]; then
            platforms+=("$platform")
        fi
    done
    
    # Generate links for each platform
    for platform in "${platforms[@]}"; do
        links+="\n### $platform\n"
        
        for file in "${files[@]}"; do
            local filename=$(basename "$file")
            local file_platform=$(detect_platform "$filename")
            
            if [ "$file_platform" = "$platform" ]; then
                local file_size=$(get_file_size "$file")
                local formatted_size=$(format_file_size "$file_size")
                local download_url="https://github.com/$RELEASE_REPO/raw/main/releases/wippy/$build_timestamp/$(basename "$file")"
                
                links+="- **$filename** ($formatted_size) - [Download]($download_url)\n"
            fi
        done
    done
    
    echo -e "$links"
}

# Function to generate Markdown content
generate_markdown() {
    local pr_number="$1"
    local commit_sha="$2"
    local short_sha="$3"
    local build_timestamp="$4"
    local artifacts_dir="$5"
    
    local ci_url=""
    if [ -n "$GITHUB_REPO" ] && [ -n "$GITHUB_RUN_ID" ]; then
        ci_url="https://github.com/$GITHUB_REPO/actions/runs/$GITHUB_RUN_ID"
    fi
    
    local artifact_links=$(generate_artifact_links "$artifacts_dir" "$pr_number" "$commit_sha" "$build_timestamp")
    
    cat << EOF
# Wippy Build Artifacts

## Build Information
- **Pull Request:** #$pr_number
- **Commit SHA:** \`$commit_sha\` (short: \`$short_sha\`)
- **Build Timestamp:** $build_timestamp
- **CI Run:** [View in GitHub Actions]($ci_url)

## Download Artifacts

$artifact_links

## Installation Instructions

### Linux
\`\`\`bash
# Download and extract
wget https://github.com/$RELEASE_REPO/raw/main/releases/wippy/$build_timestamp/wippy-*-linux-amd64
chmod +x wippy-*-linux-amd64
./wippy-*-linux-amd64 --version
\`\`\`

### Windows
\`\`\`powershell
# Download and run
Invoke-WebRequest -Uri "https://github.com/$RELEASE_REPO/raw/main/releases/wippy/$build_timestamp/wippy-*-windows-amd64.exe" -OutFile "wippy.exe"
./wippy.exe --version
\`\`\`

### macOS
\`\`\`bash
# Download and extract
curl -L https://github.com/$RELEASE_REPO/raw/main/releases/wippy/$build_timestamp/wippy-*-darwin-amd64 -o wippy
chmod +x wippy
./wippy --version
\`\`\`

---
*Generated automatically by GitHub Actions*
EOF
}

# Function to create directory structure
create_directory_structure() {
    local pr_number="$1"
    local build_timestamp="$2"
    
    local release_dir="releases/wippy"
    local timestamp_dir="$release_dir/$build_timestamp"
    
    mkdir -p "$timestamp_dir"
    echo "$timestamp_dir"
}

# Function to copy artifacts
copy_artifacts() {
    local source_dir="$1"
    local target_dir="$2"
    
    if [ ! -d "$source_dir" ]; then
        warn "Source directory $source_dir does not exist"
        return
    fi
    
    log "Copying artifacts from $source_dir to $target_dir"
    cp -r "$source_dir"/* "$target_dir/" 2>/dev/null || true
}

# Main function
main() {
    log "Starting artifact Markdown generation"
    
    # Get PR number if not provided
    if [ -z "$PR_NUMBER" ]; then
        PR_NUMBER=$(get_pr_number)
        if [ -z "$PR_NUMBER" ]; then
            error "PR number not provided and cannot be determined from GitHub context"
        fi
    fi
    
    # Get commit SHA if not provided
    if [ -z "$COMMIT_SHA" ]; then
        COMMIT_SHA=$(get_commit_sha)
        if [ -z "$COMMIT_SHA" ]; then
            error "Commit SHA not provided and cannot be determined"
        fi
    fi
    
    local short_sha=$(get_short_sha "$COMMIT_SHA")
    
    log "PR Number: $PR_NUMBER"
    log "Commit SHA: $COMMIT_SHA (short: $short_sha)"
    log "Build Timestamp: $BUILD_TIMESTAMP"
    log "Artifacts Dir: $ARTIFACTS_DIR"
    
    # Create directory structure
    local timestamp_dir=$(create_directory_structure "$PR_NUMBER" "$BUILD_TIMESTAMP")
    log "Created directory: $timestamp_dir"
    
    # Copy artifacts
    copy_artifacts "$ARTIFACTS_DIR" "$timestamp_dir"
    
    # Generate Markdown content
    local markdown_content=$(generate_markdown "$PR_NUMBER" "$COMMIT_SHA" "$short_sha" "$BUILD_TIMESTAMP" "$ARTIFACTS_DIR")
    
    # Write individual build file
    local build_file="$timestamp_dir/${BUILD_TIMESTAMP}_${short_sha}.md"
    echo "$markdown_content" > "$build_file"
    log "Created build file: $build_file"
    
    # Update latest.md
    local latest_file="releases/wippy/latest.md"
    echo "$markdown_content" > "$latest_file"
    log "Updated latest file: $latest_file"
    
    # Create symlink for easy access
    local symlink="releases/wippy/latest"
    rm -f "$symlink"
    ln -sf "$BUILD_TIMESTAMP" "$symlink"
    log "Created symlink: $symlink -> $BUILD_TIMESTAMP"
    
    log "✅ Artifact Markdown generation completed successfully"
    
    # Summary
    echo ""
    echo "📁 Files created:"
    echo "  - $build_file"
    echo "  - $latest_file"
    echo "  - $symlink (symlink)"
    echo ""
    echo "🔗 Quick links:"
    echo "  - Latest: https://github.com/$RELEASE_REPO/blob/main/releases/wippy/latest.md"
    echo "  - This build: https://github.com/$RELEASE_REPO/blob/main/releases/wippy/$BUILD_TIMESTAMP/${BUILD_TIMESTAMP}_${short_sha}.md"
}

# Run main function
main "$@"
