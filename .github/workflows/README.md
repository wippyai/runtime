# GitHub Actions Workflows

This directory contains GitHub Actions workflows for the Wippy runtime project.

## Workflows

### `tests.yml` (Integrated Tests and Build)
**Purpose:** Runs tests on all pushes/PRs, builds and publishes binaries for PRs only.

**Structure:**
- **`tests` job:** Runs on all push/PR events
- **`build-and-publish` job:** Runs only on pull requests (after tests pass)

**Triggers:** 
- Push to main/develop branches → **Tests only**
- Pull request opened/synchronized/reopened → **Tests + Build + Publish**

**What it does:**
1. **`tests` job:**
   - Runs tests on Ubuntu and Windows
   - Builds binaries for current platform (Ubuntu: all cross-compile, Windows: native)
   - Uploads build artifacts

2. **`build-and-publish` job (PR only):**
   - Downloads artifacts from tests job
   - Builds any missing binaries using Makefile
   - Packages into archives: `.tar.gz` for Linux/macOS, `.zip` for Windows
   - Publishes to `wippyai/wippy-releases` repository
   - Creates dated folder: `YYYY-MM-DD_pr-<PR_NUMBER>/`
   - Creates symlink: `latest_pr-<PR_NUMBER>/` for easy access

**Requirements:**
- `PAT_TOKEN_IGOR` secret must be configured with appropriate permissions
- Access to `wippyai/wippy-releases` repository

**Output:**
- Test results for all platforms (always)
- Binaries available in the release repository (PR only)
- Workflow artifacts uploaded for debugging
- Summary posted to PR with download links

### `linters.yml`
**Purpose:** Runs code linting on every push and pull request.

## Notes

- Cross-compilation with CGO requires specific toolchains
- Some platforms may fail to build due to missing cross-compilation tools
- The workflow continues on errors for individual platform builds
- Only successfully built binaries are packaged and published
