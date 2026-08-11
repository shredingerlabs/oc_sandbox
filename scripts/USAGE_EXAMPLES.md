# Usage Examples for create-release.sh

## Quick Start

```bash
# Navigate to project root
cd /path/to/project

# Run the script
./scripts/create-release.sh v1.0.0 "First Release" "Initial stable release" false
```

## Common Scenarios

### 1. Production Release
```bash
./scripts/create-release.sh v1.2.3 "Version 1.2.3" "Production release with bug fixes and improvements" false
```

### 2. Alpha/Beta Release
```bash
./scripts/create-release.sh v2.0.0-alpha "Alpha Release" "Early access version for testing" true
```

### 3. Release Candidate
```bash
./scripts/create-release.sh v1.5.0-rc2 "Release Candidate 2" "Second candidate for v1.5.0 production" true
```

### 4. Interactive Mode (no parameters)
```bash
./scripts/create-release.sh
# Follow the prompts for version, title, notes, etc.
```

## Release Notes Examples

### Simple
```bash
./scripts/create-release.sh v1.0.1 "Bug Fix Release" "Fixed critical authentication bug" false
```

### Detailed
```bash
./scripts/create-release.sh v2.0.0 "Major Update" "
Features:
- Complete UI redesign
- Performance improvements
- New API endpoints
- Enhanced security

Changes:
- Database migration required
- Configuration file format changed

Breaking Changes:
- Removed deprecated API v1
- Updated minimum requirements

Upgrade Guide:
1. Backup your data
2. Run migration script
3. Update configuration
4. Restart services
" false
```

## Automation Examples

### Shell Script Wrapper
```bash
#!/bin/bash
VERSION="1.2.3"
TITLE="Release $VERSION"
NOTES="Automated release from build pipeline"

./scripts/create-release.sh "$VERSION" "$TITLE" "$NOTES" false
```

### CI/CD Pipeline
```bash
# GitHub Actions example
VERSION="${GITHUB_REF#refs/tags/}"
./scripts/create-release.sh "$VERSION" "Release $VERSION" "Auto-released by CI/CD" false
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

### Test without GitHub push
```bash
# Run script to dry run
./scripts/create-release.sh v1.0.0-test "Test" "Test release" false <<EOF
y    # Continue even with uncommitted changes
n    # Don't cleanup old branches
n    # Cancel before GitHub push
EOF
```

## Version Number Patterns

```bash
v1.0.0        # Production
v1.0.0-alpha  # Alpha version
v1.0.0-beta   # Beta version  
v1.0.0-rc1    # Release candidate
v2.0.0        # Major version
v1.2.3+build  # With build metadata
v1.2.3-typescript-2024-08-10  # Custom suffix
```