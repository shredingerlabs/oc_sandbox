#!/usr/bin/env bash

# Note: Using set -e for exit on error, but avoiding set -u due to bash environment issues
set -eo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

error() {
    echo -e "${RED}Error:${NC} $1" >&2
    exit 1
}

success() {
    echo -e "${GREEN}Success:${NC} $1"
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

# Show help information
show_help() {
    cat << EOF
Usage: ./scripts/create-release.sh [FLAGS]

Create GitHub releases from dist/ folder contents.

Interactive Mode (no flags):
  ./scripts/create-release.sh

Flag-based Mode:
  --version VERSION        Version tag (e.g., v1.0.0) [REQUIRED]
  --title TITLE           Release title (default: "Release VERSION")
  --pre-release           Mark as pre-release instead of regular
  --draft                 Create as draft instead of published
  --help, -h              Show this help message

Examples:
  # Interactive mode
  ./scripts/create-release.sh

  # Published release with explicit version
  ./scripts/create-release.sh --version v1.2.3

  # Published release with custom title
  ./scripts/create-release.sh --version v1.2.3 --title "Major Release"

  # Pre-release as draft
  ./scripts/create-release.sh --version v2.0.0-beta --pre-release --draft

  # Regular published release
  ./scripts/create-release.sh --version v1.0.0

Notes:
- Releases are published by default (not drafts)
- Use --draft for review before publishing
- Release notes are only collected in interactive mode
EOF
}

# Check gh CLI authentication
check_gh_auth() {
    if ! command -v gh &> /dev/null; then
        error "GitHub CLI (gh) not installed. Install from https://cli.github.com/"
    fi
    
    if ! gh auth status &> /dev/null; then
        error "GitHub CLI not authenticated. Run 'gh auth login' first."
    fi
    
    success "GitHub CLI authenticated"
}

# Check dist folder
check_dist_folder() {
    if [ ! -d "dist" ]; then
        error "dist/ directory not found in current directory"
    fi
    
    if [ -z "$(ls -A dist)" ]; then
        error "dist/ directory is empty"
    fi
    
    local file_count=$(find dist -type f | wc -l)
    success "dist/ directory found with $file_count files"
}

# Check git status
check_git_status() {
    local status=$(git status --porcelain 2>/dev/null || echo "")
    
    if [ -n "$status" ]; then
        warn "Working directory has uncommitted changes:"
        echo "$status"
        echo ""
        
        read -p "Do you want to continue anyway? (y/N): " proceed
        if [[ ! "$proceed" =~ ^[Yy]$ ]]; then
            error "Release cancelled by user"
        fi
    fi
    
    local current_branch=$(git branch --show-current)
    success "Current branch: $current_branch"
}

# Get current directory for cleanup
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Find project root (script is in scripts/, so go up one level)
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Check for existing tags
check_existing_tags() {
    local existing_tags=$(git tag --list | grep "^$version$" || true)
    
    if [ -n "$existing_tags" ]; then
        error "Tag $version already exists. Use a different version."
    fi
}

# Clean up old release branches
cleanup_old_branches() {
    local current_branch=$(git branch --show-current)
    
    # Get all local release-dist branches, excluding current one
    local branches=$(git branch --list 'release-dist-*' 2>/dev/null | grep -v "$current_branch" || true)
    
    if [ -n "$branches" ]; then
        local branch_count=$(echo "$branches" | wc -l)
        warn "Found $branch_count old local release branch(es):"
        echo "$branches"
        echo ""
        
        read -p "Clean up local old release branches? (y/N): " cleanup
        if [[ "$cleanup" =~ ^[Yy]$ ]]; then
            while IFS= read -r branch; do
                if [ -n "$branch" ]; then
                    echo "Deleting local branch: $branch"
                    git branch -D "$branch" 2>/dev/null || warn "Failed to delete $branch"
                fi
            done <<< "$branches"
            success "Cleaned up old local branches"
        fi
    fi
    
    # Show existing remote temp branches (manual cleanup required)
    local remote_branches=$(git branch -r 2>/dev/null | grep 'release-dist-' || true)
    if [ -n "$remote_branches" ]; then
        echo ""
        warn "The following remote release branches exist (manual cleanup required):"
        echo "$remote_branches"
        echo ""
        echo "To remove them manually:"
        echo "  git push origin --delete <branch-name>"
    fi
}

# Interactive mode
interactive_mode() {
    # Get version tag with validation
    read -p "Enter version tag (e.g., v1.0.0): " version
    if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
        error "Invalid version format. Expected format: v1.2.3"
    fi
    
    # Get release title
    read -p "Enter release title (optional, press Enter to use version): " title
    if [ -z "$title" ]; then
        title="Release $version"
    fi
    
    # Get release notes (interactive multi-line input)
    echo "Enter release notes (Ctrl+D when done):"
    release_notes=""
    while IFS= read -r line; do
        release_notes+="$line"$'\n'
    done
    
    if [ -z "$release_notes" ]; then
        warn "No release notes provided"
        release_notes="Release $version from dist/ folder"
    fi
    
    # Get pre-release preference
    read -p "Mark as pre-release? (y/N): " pre_release
    if [[ "$pre_release" =~ ^[Yy]$ ]]; then
        is_pre_release=true
        warn "This will be marked as a pre-release"
    else
        is_pre_release=false
    fi
    
    # Get draft preference
    read -p "Create as draft? (y/N): " draft
    if [[ "$draft" =~ ^[Yy]$ ]]; then
        is_draft=true
        warn "This will be created as a draft release"
    else
        is_draft=false
    fi
}

# Flag-based mode
flag_based_mode() {
    # Validate required flags
    if [ -z "${version:-}" ]; then
        error "--version flag is required. Use --help for usage information."
    fi
    
    # Validate version format
    if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
        error "Invalid version format. Expected format: v1.2.3"
    fi
    
    # Set defaults for optional parameters
    if [ -z "${title:-}" ]; then
        title="Release $version"
    fi
    
    # Set defaults for boolean flags
    if [ -z "${is_pre_release:-}" ]; then
        is_pre_release=false
    fi
    
    if [ -z "${is_draft:-}" ]; then
        is_draft=false
    fi
    
    # Set default release notes for flag mode
    if [ -z "${release_notes:-}" ]; then
        release_notes="Release $version from dist/ folder"
    fi
}

# Common release creation logic
create_release() {
    # Pre-flight checks
    check_gh_auth
    check_dist_folder
    check_git_status
    
    # Check for existing tags
    local existing_tags=$(git tag --list | grep "^$version$" || true)
    if [ -n "$existing_tags" ]; then
        error "Tag $version already exists. Use a different version."
    fi
    success "Version tag: $version"
    
    # Setup cleanup trap
    trap cleanup_on_exit EXIT
    
    # Clean up old branches first
    cleanup_old_branches
    
    # Confirm before proceeding
    echo ""
    echo "=== Release Summary ==="
    echo "Version: $version"
    echo "Title: $title"
    echo "Pre-release: $is_pre_release"
    echo "Draft: $is_draft"
    echo "Source: dist/ folder"
    echo ""
    
    read -p "Proceed with release creation? (y/N): " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        error "Release cancelled by user"
    fi
    
    # Create temporary orphan branch
    local temp_branch="release-dist-${version}"
    success "Creating temporary orphan branch: $temp_branch"
    
    git checkout --orphan "$temp_branch"
    
    # Remove everything except dist folder
    success "Removing non-dist files from orphan branch"
    find . -maxdepth 1 -not -name '.' -not -name '..' -not -name 'dist' -not -name '.git' -exec rm -rf {} + 2>/dev/null || true
    
    # Move dist contents to root level
    success "Moving dist/ contents to root level"
    if ! mv dist/* . 2>/dev/null; then
        error "Failed to move dist/ contents to root level"
    fi
    
    # Handle hidden files in dist (excluding . and ..)
    if [ -d dist ] && [ "$(ls -A dist)" ]; then
        find dist -maxdepth 1 -name '.*' -not -name '.' -not -name '..' -exec mv {} . \;
    fi
    
    # Remove the now-empty dist folder
    rm -rf dist
    
    # Verify we have content before proceeding
    if [ -z "$(ls -A)" ]; then
        error "No files were found in dist/ folder. Release branch is empty."
    fi
    
    git add .
    
    # Commit with release info
    local commit_msg="Release $version from dist/ folder
    
Release generated from dist/ contents."
    
    git commit -m "$commit_msg"
    success "Created release commit"
    
    # Tag the commit
    git tag "$version"
    success "Created tag: $version"
    
    # Push to GitHub
    success "Pushing to GitHub..."
    git push origin "$temp_branch"
    git push origin "$version"
    success "Pushed branch and tag"
    
    # Build release flags based on user choices
    local release_flags=("--title" "$title" "--notes" "$release_notes")
    
    # Only add draft flag if explicitly requested
    if [ "$is_draft" = true ]; then
        release_flags+=("--draft")
    fi
    
    # Add pre-release flag if requested
    if [ "$is_pre_release" = true ]; then
        release_flags+=("--prerelease")
    fi
    
    success "Creating GitHub release..."
    local release_url=$(gh release create "$version" "${release_flags[@]}")
    
    # Return to original branch with better error handling
    success "Returning to original branch: $original_branch"
    if ! git checkout "$original_branch"; then
        error "Failed to return to original branch '$original_branch'. You're currently on branch '$temp_branch'"
    fi
    
    # Clean up temporary branch locally
    success "Cleaning up temporary branch: $temp_branch"
    if ! git branch -D "$temp_branch" 2>/dev/null; then
        warn "Failed to delete local temporary branch '$temp_branch'. Manual cleanup may be needed."
    else
        success "Cleaned up temporary branch"
    fi
    
    # Final summary
    echo ""
    echo "=== Release Created Successfully ==="
    echo "Version: $version"
    echo "Release URL: $release_url"
    if [ "$is_draft" = true ]; then
        echo "Status: Draft (needs manual publishing)"
    else
        echo "Status: Published"
    fi
    echo ""
    echo "Next steps:"
    if [ "$is_draft" = true ]; then
        echo "1. Review the release at: $release_url"
        echo "2. Click 'Publish release' when ready"
    else
        echo "1. The release is now live at: $release_url"
    fi
    echo "2. The release contains only files from the dist/ folder"
}

# Parse command-line flags
parse_flags() {
    local flag_count=0
    
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --version)
                version="$2"
                shift 2
                ((flag_count+=1))
                ;;
            --title)
                title="$2"
                shift 2
                ((flag_count+=1))
                ;;
            --pre-release)
                is_pre_release=true
                shift
                ((flag_count+=1))
                ;;
            --draft)
                is_draft=true
                shift
                ((flag_count+=1))
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                error "Unknown option: $1. Use --help for available options."
                ;;
        esac
    done
    
    # Return flag_count via global variable for reliability
    FLAG_COUNT=$flag_count
    return 0
}

# Cleanup on exit
cleanup_on_exit() {
    local exit_code=$?
    if [ $exit_code -ne 0 ]; then
        warn "Script exited with error code: $exit_code"
        local current_branch=$(git branch --show-current 2>/dev/null || echo "")
        if [ -n "${temp_branch:-}" ] && [ "$current_branch" = "$temp_branch" ]; then
            echo ""
            warn "You're currently on temporary branch '$temp_branch'"
            echo "To return to '${original_branch:-main}': git checkout ${original_branch:-main}"
            echo "To delete this branch: git branch -D $temp_branch"
        fi
    fi
}

# Main execution
main() {
    cd "$PROJECT_ROOT"
    
    # Store original branch before any git operations
    local original_branch
    original_branch=$(git branch --show-current)
    
    # Parse command-line flags
    parse_flags "$@"
    
    # Check if we should use interactive mode or flag-based mode
    if [ "${FLAG_COUNT:-0}" -eq 0 ]; then
        interactive_mode
    else
        flag_based_mode
    fi
    
    # Common release creation
    create_release
}

# Run main function with arguments
main "$@"