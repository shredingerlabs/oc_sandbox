# Usage Examples for create-release.sh

## Quick Start

```bash
# Navigate to project root
cd /path/to/project

# Interactive mode (with release notes)
./scripts/create-release.sh
```

## Common Scenarios

### 1. Production Release (Published)
```bash
./scripts/create-release.sh --version v1.2.3
```

### 2. Production Release with Custom Title
```bash
./scripts/create-release.sh --version v1.2.3 --title "Version 1.2.3"
```

### 3. Alpha/Beta Pre-release (Published)
```bash
./scripts/create-release.sh --version v2.0.0-alpha --pre-release
```

### 4. Release Candidate as Draft
```bash
./scripts/create-release.sh --version v1.5.0-rc2 --pre-release --draft
```

### 5. Draft Release for Review
```bash
./scripts/create-release.sh --version v1.0.0 --draft
```

### 6. Interactive Mode (no flags)
```bash
./scripts/create-release.sh
# Follow the prompts for version, title, notes, pre-release, draft
```

## Flag Combinations

### Basic Published Releases
```bash
# Simple version
./scripts/create-release.sh --version v1.0.0

# With custom title
./scripts/create-release.sh --version v1.0.0 --title "First Release"

# Multiple flags
./scripts/create-release.sh --version v1.0.0 --title "Major Update"
```

### Pre-releases
```bash
# Published pre-release
./scripts/create-release.sh --version v2.0.0-beta --pre-release

# Draft pre-release
./scripts/create-release.sh --version v2.0.0-beta --pre-release --draft

# Alpha version with title
./scripts/create-release.sh --version v3.0.0-alpha --title "Alpha Test Version" --pre-release
```

### Draft Releases
```bash
# Draft for review
./scripts/create-release.sh --version v1.2.3 --draft

# Draft with custom title
./scripts/create-release.sh --version v1.2.3 --title "Bug Fix Release" --draft
```

## Version Format Examples

```bash
# Standard semantic versioning
v1.0.0
v1.2.3
v2.0.0

# Pre-release versions
v1.0.0-alpha
v1.0.0-beta
v1.0.0-rc1
v2.0.0-beta.1
v1.2.3-alpha

# Build metadata
v1.0.0+build123
v1.2.3+20240811

# Custom suffixes
v1.2.3-rc1
v2.0.0-alpha
v1.0.0-beta2
```

## Error Handling Examples

### Invalid Version Format
```bash
./scripts/create-release.sh --version 1.0.0
# Error: Invalid version format. Expected format: v1.2.3

./scripts/create-release.sh --version v1.0
# Error: Invalid version format. Expected format: v1.2.3
```

### Missing Required Flags
```bash
./scripts/create-release.sh --title "Release Title"
# Error: --version flag is required. Use --help for usage information.
```

### Unknown Flags
```bash
./scripts/create-release.sh --version v1.0.0 --unknown-flag
# Error: Unknown option: --unknown-flag. Use --help for available options.
```

## Automation Examples

### Shell Script Wrapper
```bash
#!/bin/bash
VERSION="1.2.3"
TITLE="Release $VERSION"

./scripts/create-release.sh --version "$VERSION" --title "$TITLE"
```

### CI/CD Pipeline
```bash
# GitHub Actions example
VERSION="${GITHUB_REF#refs/tags/}"
./scripts/create-release.sh --version "$VERSION" --title "Release $VERSION"
```

### Release Script with Custom Logic
```bash
#!/bin/bash
# Auto-determine version from git tag
VERSION=$(git describe --tags --abbrev=0)

# Determine if pre-release based on version string
if [[ "$VERSION" =~ -(alpha|beta|rc) ]]; then
    ./scripts/create-release.sh --version "$VERSION" --pre-release --draft
else
    ./scripts/create-release.sh --version "$VERSION"
fi
```

## Interactive Mode Benefits

The interactive mode (`./scripts/create-release.sh` with no flags) provides:

- **Multi-line release notes** - Enter detailed release notes interactively
- **Guided workflow** - Step-by-step prompts
- **Confirmation** - Review before proceeding
- **Default behaviors** - Sensible defaults for optional fields

```bash
./scripts/create-release.sh
# You'll be prompted for:
# 1. Version tag (e.g., v1.0.0)
# 2. Release title (optional)
# 3. Release notes (Ctrl+D when done)
# 4. Pre-release confirmation (y/N)
# 5. Draft confirmation (y/N)
```

## Troubleshooting Commands

### Clean up failed releases
```bash
# Delete local tags
git tag -d v1.0.0

# Delete GitHub releases
gh release delete v1.0.0

# Clean remote tags
git push origin :refs/tags/v1.0.0

# Clean up branches
git branch -D release-dist-v1.0.0
```

### Check existing tags
```bash
git tag --list
gh release list
```

### View dist folder contents
```bash
ls -la dist/
find dist -type f | wc -l
```

## Release Examples by Use Case

### Bug Fix Release
```bash
./scripts/create-release.sh \
  --version v1.2.1 \
  --title "Bug Fix Release"
```

### Feature Release
```bash
./scripts/create-release.sh \
  --version v2.0.0 \
  --title "Major Feature Release"
```

### Security Release
```bash
./scripts/create-release.sh \
  --version v1.2.3 \
  --title "Security Patch"
```

### Beta Testing Release
```bash
./scripts/create-release.sh \
  --version v2.0.0-beta \
  --title "Beta Testing" \
  --pre-release \
  --draft
```

### First Public Release
```bash
./scripts/create-release.sh \
  --version v1.0.0 \
  --title "Initial Public Release"
```

## Regional/Team Release Examples

### German Team
```bash
./scripts/create-release.sh \
  --version v1.0.0 \
  --title "Erste Version"
```

### Documentation Release
```bash
./scripts/create-release.sh \
  --version v1.2.3-doc \
  --title "Documentation Update"
```

### Internal Alpha Release
```bash
./scripts/create-release.sh \
  --version v2.0.0-internal-alpha \
  --title "Internal Alpha Test" \
  --pre-release
```

## Progressive Release Strategy

### 1. Alpha Release (Draft)
```bash
./scripts/create-release.sh --version v2.0.0-alpha --pre-release --draft
```

### 2. Beta Release (Published for testing)
```bash
./scripts/create-release.sh --version v2.0.0-beta --pre-release
```

### 3. Release Candidate (Published for final testing)
```bash
./scripts/create-release.sh --version v2.0.0-rc1 --pre-release
```

### 4. Final Release (Published)
```bash
./scripts/create-release.sh --version v2.0.0
```

## Version Number Patterns

The script follows Semantic Versioning (SemVer):

```bash
vMAJOR.MINOR.PATCH

# Examples:
v1.0.0    # Initial release
v1.2.3    # Standard release
v2.0.0    # Major version change
v1.5.2    # Patch release

# Pre-release components:
v1.0.0-alpha
v1.0.0-alpha.1
v1.0.0-beta
v1.0.0-beta.2
v1.0.0-rc.1
v1.0.0-rc2
```

## Quick Reference

| Scenario | Command |
|----------|---------|
| Simple release | `./scripts/create-release.sh --version v1.0.0` |
| Custom title | `./scripts/create-release.sh --version v1.0.0 --title "My Release"` |
| Pre-release | `./scripts/create-release.sh --version v1.0.0-beta --pre-release` |
| Draft for review | `./scripts/create-release.sh --version v1.0.0 --draft` |
| Interactive | `./scripts/create-release.sh` |
| Help | `./scripts/create-release.sh --help` |