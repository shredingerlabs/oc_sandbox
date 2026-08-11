# create-release.sh - Git Release Script from Dist Folder

Automated script to create GitHub releases from the `dist/` folder contents only.

## Features

- ✅ **Semantic version validation** - Validates version tags (v1.2.3 format)
- ✅ **Interactive or automated** - Use via prompts or command-line arguments
- ✅ **Release notes support** - Multi-line release notes input
- ✅ **Pre-release flag** - Mark releases as pre-release
- ✅ **Clean history** - Creates orphan branch with only dist/ contents
- ✅ **Auto-cleanup** - Removes old release branches automatically
- ✅ **Draft releases** - Creates draft releases for review before publishing
- ✅ **Safety checks** - Validates git status, dist folder, and authentication

## Usage

### Interactive Mode

```bash
./dist/scripts/create-release.sh
```

Prompts for:
1. Version tag (e.g., v1.0.0)
2. Release title (optional)
3. Release notes (multi-line)
4. Pre-release confirmation (y/N)
5. Final confirmation

### Command-Line Mode

```bash
./dist/scripts/create-release.sh v1.0.0 "Release Title" "Release notes" false
```

#### Parameters

- `$1` - **Version tag** (required) - Format: `v1.2.3` or `v1.2.3-beta`
- `$2` - **Title** (optional) - Default: "Release v1.0.0"
- `$3` - **Release notes** (optional) - Default: Basic release message
- `$4` - **Pre-release** (optional) - `true`/`false`, default: false

## Examples

### Basic Release

```bash
./dist/scripts/create-release.sh v1.0.0 "First Release" "Initial stable release of our project" false
```

### Pre-release

```bash
./dist/scripts/create-release.sh v1.0.0-rc1 "Release Candidate 1" "Testing release candidate 1" true
```

### Beta Release with detailed notes

```bash
./dist/scripts/create-release.sh v1.0.0-beta "Beta Release" "Features:
- User authentication
- Dashboard improvements
- Bug fixes

Known issues:
- Mobile view needs work" false
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
   - Copy dist/ contents to root
   - Commit with release message
   - Create version tag

4. **GitHub Operations**
   - Push branch and tag to GitHub
   - Create draft GitHub release with notes
   - Auto-cleanup local temporary branch

5. **Cleanup**
   - Return to original branch
   - Clean up old release branches
   - Display release URL for publishing

## Release Process

1. **Create Draft Release**
   ```bash
   ./dist/scripts/create-release.sh v1.0.0 "Release Title" "Release notes" false
   ```

2. **Review Release**
   - Visit the provided GitHub URL
   - Review assets and release notes
   - Make any necessary edits

3. **Publish Release**
   - Click "Publish release" on GitHub
   - Release becomes publicly visible

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

### Success Example
```
=== Release Created Successfully ===
Version: v1.0.0
Release URL: https://github.com/user/repo/releases/tag/v1.0.0
Status: Draft (needs manual publishing)

Next steps:
1. Review the release at: https://github.com/user/repo/releases/tag/v1.0.0
2. Click 'Publish release' when ready
3. The release will contain only files from the dist/ folder
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
- **Draft releases** - Final review before publishing
- **Rollback support** - Returns to original branch on failure

## Integration with CI/CD

Can be integrated with CI/CD pipelines for automated releases:

```yaml
# Example GitHub Actions workflow
- name: Create Release
  run: |
    ./dist/scripts/create-release.sh v${{ github.ref_name }} "Release ${{ github.ref_name }}" "Auto-release from CI/CD" false
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

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