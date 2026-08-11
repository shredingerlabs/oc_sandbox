# create-release.sh - Git Release Script from Dist Folder

Automated script to create GitHub releases from the `dist/` folder contents only.

## Features

- ✅ **Semantic version validation** - Validates version tags (v1.2.3 format)
- ✅ **Interactive or automated** - Use via prompts or flags
- ✅ **Release notes support** - Multi-line release notes input (interactive mode only)
- ✅ **Pre-release flag** - Mark releases as pre-release
- ✅ **Clean history** - Creates orphan branch with only dist/ contents
- ✅ **Auto-cleanup** - Removes old release branches automatically
- ✅ **Optional draft releases** - Creates draft releases for review before publishing
- ✅ **Safety checks** - Validates git status, dist folder, and authentication

## Usage

### Interactive Mode (no flags)
```bash
./scripts/create-release.sh
```

Prompts for:
1. Version tag (e.g., v1.0.0)
2. Release title (optional)
3. Release notes (multi-line)
4. Pre-release confirmation (y/N)
5. Draft confirmation (y/N)

### Flag-based Mode
```bash
./scripts/create-release.sh --version v1.0.0
```

#### Flags

- `--version VERSION` - Version tag (required, format: v1.2.3)
- `--title TITLE` - Release title (optional, default: "Release v1.0.0")
- `--pre-release` - Mark as pre-release (presence = true)
- `--draft` - Create as draft instead of published (presence = true)
- `--help, -h` - Show usage information

## Examples

### Interactive Mode (with release notes)
```bash
./scripts/create-release.sh
```

### Basic Published Release
```bash
./scripts/create-release.sh --version v1.0.0
```

### Published Release with Custom Title
```bash
./scripts/create-release.sh --version v1.0.0 --title "Major Release"
```

### Pre-release
```bash
./scripts/create-release.sh --version v2.0.0-beta --pre-release
```

### Draft Release
```bash
./scripts/create-release.sh --version v1.0.0 --draft
```

### Pre-release as Draft
```bash
./scripts/create-release.sh --version v2.0.0-beta --pre-release --draft
```

## Prerequisites

1. **GitHub CLI** installed and authenticated:
    ```bash
    gh auth login
    ```

2. **Git repository** with `dist/` folder present

3. **Clean dist folder** (recommended but not required)

## Script Workflow

1. **Pre-flight Checks**
    - GitHub CLI authentication
    - Dist folder existence & contents
    - Git working directory status

2. **Version Validation**
    - Semantic version format check: `v` + numbers + `.` + numbers + `.` + numbers
    - Existing tag verification

3. **Release Creation**
    - Create temporary orphan branch: `release-dist-v<version>`
    - Move dist/ contents to root
    - Commit with release message
    - Create version tag

4. **GitHub Operations**
    - Push branch and tag to GitHub
    - Create GitHub release (published or draft based on --draft flag)
    - Auto-cleanup local temporary branch

5. **Cleanup**
    - Return to original branch
    - Clean up old release branches
    - Display release URL

## Release Process

### Published Release
```bash
./scripts/create-release.sh --version v1.0.0
# Release is immediately published on GitHub
```

### Draft Release (for review)
```bash
./scripts/create-release.sh --version v1.0.0 --draft
# 1. Visit the provided GitHub URL
# 2. Review assets and release notes
# 3. Edit if necessary
# 4. Click "Publish release" when ready
```

## Troubleshooting

### "Tag already exists"
```bash
git tag -d v1.0.0           # Delete local tag
gh release delete v1.0.0    # Delete GitHub release
git push origin :refs/tags/v1.0.0  # Remove remote tag
```

### "Invalid version format"
Ensure version follows semantic versioning: `v1.2.3` or `v1.2.3-beta`

### "GitHub CLI not authenticated"
```bash
gh auth login
```

### "Working directory has uncommitted changes"
- Commit pending changes first
- Or proceed with caution (script will prompt)

## Output

### Published Release Example
```
=== Release Created Successfully ===
Version: v1.0.0
Release URL: https://github.com/user/repo/releases/tag/v1.0.0
Status: Published

Next steps:
1. The release is now live at: https://github.com/user/repo/releases/tag/v1.0.0
2. The release contains only files from the dist/ folder
```

### Draft Release Example
```
=== Release Created Successfully ===
Version: v1.0.0
Release URL: https://github.com/user/repo/releases/tag/v1.0.0
Status: Draft (needs manual publishing)

Next steps:
1. Review the release at: https://github.com/user/repo/releases/tag/v1.0.0
2. Click 'Publish release' when ready
```

## Branch Naming

Script uses auto-generated branch names:
- Format: `release-dist-v<version>`
- Example: `release-dist-v1.0.0`
- Automatically cleaned up after release creation

## Safety Features

- **Clean working tree check** - Warns about uncommitted changes
- **Version validation** - Ensures proper semantic versioning
- **Tag collision detection** - Prevents duplicate tags
- **Branch cleanup** - Removes old release branches automatically
- **Optional draft releases** - Review before publishing
- **Rollback support** - Returns to original branch on failure
- **Exit trap** - Provides recovery commands if errors occur

## Integration with CI/CD

Can be integrated with CI/CD pipelines for automated releases:

```yaml
# Example GitHub Actions workflow
- name: Create Release
  run: |
    ./scripts/create-release.sh --version v${{ github.ref_name }} --title "Release ${{ github.ref_name }}"
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**Important**: Release notes are only available in interactive mode. For CI/CD, use custom release notes or let them default.

## Exit Codes

- `0` - Success
- `1` - Error (validation, authentication, or git operation failure)

## Files Released

The script automatically includes all files from the `dist/` folder:
- Build artifacts
- Compiled binaries
- Documentation
- Templates
- Scripts

Excludes:
- Source code (outside dist/)
- Development files
- Test files
- Configuration files

## Default Behavior

- **Published releases** - All releases are published by default (not drafts)
- **Regular releases** - All releases are regular by default (not pre-releases)
- **Auto-title** - Uses "Release v1.0.0" if no title provided
- **Default notes** - Uses "Release v1.0.0 from dist/ folder" if no notes provided (flag mode)
- **Branch cleanup** - Automatically cleans up local release branches
- **Safety checks** - Validates before proceeding