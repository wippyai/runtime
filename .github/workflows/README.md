# GitHub Actions Workflows

This directory contains GitHub Actions workflows for the Wippy runtime project.

## Workflows

### `tests.yml` (Integrated Build)
**Purpose:** Runs tests AND builds cross-platform binaries for pull requests.

**Triggers:** 
- Push to main/develop branches
- Pull request opened/synchronized/reopened

**What it does:**
1. **Runs tests** on Ubuntu and Windows
2. **Builds binaries** for all supported platforms:
   - Linux: amd64 (native), arm64 (cross-compile)
   - Windows: amd64 (native), arm64 (cross-compile)  
   - macOS: amd64, arm64 (cross-compile)

3. **Packages and publishes** (pull requests only):
   - Creates archives: `.tar.gz` for Linux/macOS, `.zip` for Windows
   - Publishes to `wippyai/wippy-releases` repository
   - Creates dated folder: `YYYY-MM-DD_pr-<PR_NUMBER>/`
   - Creates symlink: `latest_pr-<PR_NUMBER>/` for easy access

**Requirements:**
- `PAT_TOKEN_IGOR` secret must be configured with appropriate permissions
- Access to `wippyai/wippy-releases` repository

**Output:**
- Test results for all platforms
- Binaries available in the release repository (PR only)
- Workflow artifacts uploaded for debugging
- Summary posted to PR with download links

### `linters.yml`
**Purpose:** Runs code linting on every push and pull request.

### `tests.yml`
**Purpose:** Runs tests on multiple platforms (Ubuntu, Windows) for every push and pull request.

## Notes

- Cross-compilation with CGO requires specific toolchains
- Some platforms may fail to build due to missing cross-compilation tools
- The workflow continues on errors for individual platform builds
- Only successfully built binaries are packaged and published
